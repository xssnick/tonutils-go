package tvm

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type TransactionEmulationConfig = MessageEmulationConfig

type TransactionExecutionResult struct {
	ExecutionResult
	Accepted         bool
	Transaction      *tlb.Transaction
	TransactionCell  *cell.Cell
	AccountState     *tlb.AccountState
	AccountCell      *cell.Cell
	ShardAccount     *tlb.ShardAccount
	ShardAccountCell *cell.Cell
}

type transactionRuntimeAccount struct {
	addr            *address.Address
	status          tlb.AccountStatus
	storageInfo     tlb.StorageInfo
	balance         *big.Int
	extraCurrencies *cell.Dictionary
	code            *cell.Cell
	data            *cell.Cell
	libraries       *cell.Dictionary
	tickTock        *tlb.TickTock
	stateHash       []byte
	storageLT       uint64
	storageCell     *cell.Cell
	prevTxHash      []byte
	prevTxLT        uint64
	originalCell    *cell.Cell
}

type transactionUsage struct {
	cells uint64
	bits  uint64
}

type transactionStorageDueLimits struct {
	freezeDue *big.Int
	deleteDue *big.Int
}

type transactionPreparedPhases struct {
	balance         *big.Int
	extraCurrencies *cell.Dictionary
	msgBalance      *transactionCurrencyBalance
	creditPhase     *tlb.CreditPhase
	creditFirst     bool
	storagePhase    *tlb.StoragePhase
	status          tlb.AccountStatus
	deleted         bool
	duePayment      *tlb.Coins
	lastPaid        uint32
}

type transactionCurrencyBalance struct {
	grams *big.Int
	extra map[uint32]*big.Int
}

type transactionSizeLimits struct {
	maxMsgBits            uint64
	maxMsgCells           uint64
	maxLibraryCells       uint64
	maxAccStateCells      uint64
	maxMCAccStateCells    uint64
	maxAccPublicLibraries uint64
	maxMsgExtraCurrencies uint64
}

func (tvm *TVM) EmulateTransaction(shard *tlb.ShardAccount, msgCell *cell.Cell, cfg TransactionEmulationConfig) (*TransactionExecutionResult, error) {
	if shard == nil {
		return nil, errors.New("shard account is required")
	}
	if msgCell == nil {
		return nil, errors.New("input message is required")
	}

	var msg tlb.Message
	if err := tlb.LoadFromCell(&msg, msgCell.MustBeginParse()); err != nil {
		return nil, fmt.Errorf("failed to decode input message: %w", err)
	}
	if msg.MsgType == tlb.MsgTypeExternalOut {
		return nil, errors.New("external outbound messages cannot be used as transaction input")
	}

	var proof *cell.MerkleProofBuilder
	if cfg.BuildProof {
		if shard.Account == nil {
			return nil, errors.New("shard account root is nil")
		}
		proof = cell.NewMerkleProofBuilder(shard.Account)
	}

	runtimeAcc, err := loadTransactionRuntimeAccount(shard, msg.Msg.DestAddr(), proof)
	if err != nil {
		return nil, err
	}

	now := cfg.Now
	if now == 0 {
		now = uint32(time.Now().Unix())
	}

	blockchainCfg := tlb.BlockchainConfig{
		Root: cfg.ConfigRoot,
	}

	storageDueLimits := transactionGetStorageDueLimits(blockchainCfg, runtimeAcc.addr)

	storageFee, err := transactionComputeStorageFee(blockchainCfg, runtimeAcc, now)
	if err != nil {
		return nil, err
	}
	importFee := transactionComputeImportFee(blockchainCfg, runtimeAcc.addr, &msg, msgCell)
	prepared, err := transactionPrepareInitialPhases(runtimeAcc, &msg, storageFee, importFee, now, storageDueLimits)
	if err != nil {
		return nil, err
	}

	execCfg := cfg
	execCfg.Address = runtimeAcc.addr
	execCfg.Now = now
	if execCfg.BlockLT == 0 {
		execCfg.BlockLT = execCfg.LogicalTime
	}
	execCfg.Balance = new(big.Int).Set(prepared.balance)

	computeAcc := runtimeAcc
	msgStateUsed := false
	var skipReason *tlb.ComputeSkipReason
	var gas vm.Gas
	if prepared.balance.Sign() <= 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	} else {
		gas = transactionMessageGas(execCfg, blockchainCfg, runtimeAcc.addr, prepared.balance, prepared.msgBalance.grams, msg.MsgType)
		if gas.Limit == 0 && gas.Credit == 0 {
			skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
		} else {
			computeAcc, msgStateUsed, skipReason, err = transactionPrepareComputeAccount(runtimeAcc, prepared.status, prepared.deleted, &msg)
			if err != nil {
				return nil, err
			}
			if cfg.BuildProof && msgStateUsed && skipReason == nil {
				return nil, errors.New("account execution proof cannot be built for code loaded from message state init")
			}
		}
	}

	var msgRes *MessageExecutionResult
	accountActivated := false
	if skipReason == nil {
		msgRes, err = tvm.executeTransactionMessage(computeAcc, msgCell, &msg, execCfg, gas, prepared.msgBalance.grams, proof)
		if err != nil {
			return nil, err
		}
		if msgStateUsed && msgRes.Accepted {
			accountActivated = true
			prepared.status = tlb.AccountStatusActive
			prepared.deleted = false
		}
	}

	out := &TransactionExecutionResult{}
	if msgRes != nil {
		out.ExecutionResult = msgRes.ExecutionResult
		out.Accepted = msgRes.Accepted
	}
	if msg.MsgType == tlb.MsgTypeExternalIn && (msgRes == nil || !msgRes.Accepted) {
		return out, nil
	}

	startLT := transactionStartLT(runtimeAcc.storageLT, execCfg.LogicalTime, &msg)
	endLT := startLT + 1
	nextCode := computeAcc.code
	nextData := computeAcc.data
	nextLibraries := computeAcc.libraries
	nextExtraCurrencies := prepared.extraCurrencies
	var outMessages []*cell.Cell
	var actionPhase *tlb.ActionPhase
	var actionBounce bool
	var actionDeleted bool
	actionFine := big.NewInt(0)
	gasFees := big.NewInt(0)
	if msgRes != nil && msgRes.Accepted {
		gasFees = transactionComputeGasFee(blockchainCfg, runtimeAcc.addr, uint64(msgRes.GasUsed))
	}
	finalBalance := new(big.Int).Sub(prepared.balance, gasFees)
	if finalBalance.Sign() < 0 {
		return nil, errors.New("transaction fees exceed account balance")
	}

	actionFees := big.NewInt(0)
	if msgRes != nil {
		actionRes, applyErr := transactionApplyActions(computeAcc, msgRes, startLT, now, blockchainCfg, finalBalance, nextExtraCurrencies, prepared.msgBalance, gasFees)
		if applyErr != nil {
			return nil, applyErr
		}
		outMessages = actionRes.outMsgs
		actionPhase = actionRes.phase
		nextCode = actionRes.nextCode
		nextLibraries = actionRes.nextLibraries
		nextExtraCurrencies = actionRes.extraCurrencies
		endLT = actionRes.endLT
		finalBalance = actionRes.balance
		actionFees = actionRes.actionFees
		actionFine = actionRes.actionFine
		actionBounce = actionRes.bounce
		actionDeleted = actionRes.deleteAccount
	}

	totalFees := new(big.Int).Set(prepared.storagePhase.StorageFeesCollected.Nano())
	totalFees.Add(totalFees, gasFees)
	totalFees.Add(totalFees, importFee)
	totalFees.Add(totalFees, actionFees)

	computeSuccess := msgRes != nil && vm.IsSuccessExitCode(msgRes.ExitCode)
	actionSuccess := actionPhase != nil && actionPhase.Success
	aborted := skipReason != nil || !(computeSuccess && actionSuccess)
	var bouncePhase *tlb.BouncePhase
	if aborted && transactionShouldBounce(&msg, skipReason, computeSuccess, actionBounce) {
		bounceRes, bounceErr := transactionPrepareBouncePhase(&msg, finalBalance, nextExtraCurrencies, prepared.msgBalance, gasFees, actionFine, startLT, now, len(outMessages), blockchainCfg, skipReason, msgRes, actionPhase)
		if bounceErr != nil {
			return nil, bounceErr
		}
		if bounceRes != nil {
			bouncePhase = bounceRes.phase
			finalBalance = bounceRes.balance
			nextExtraCurrencies = bounceRes.extraCurrencies
			totalFees.Add(totalFees, bounceRes.msgFees)
			if bounceRes.outMsg != nil {
				outMessages = append(outMessages, bounceRes.outMsg)
				endLT++
			}
		}
	}

	if computeSuccess && actionSuccess {
		nextData = msgRes.Data
	}
	if nextCode == nil {
		nextCode = computeAcc.code
	}
	if nextLibraries == nil {
		nextLibraries = computeAcc.libraries
	}

	if actionDeleted {
		prepared.deleted = true
	}
	finalStatus := transactionFinalizeAccountStatus(prepared.status, prepared.deleted, finalBalance, nextExtraCurrencies, accountActivated)
	nextStateHash := runtimeAcc.stateHash
	if finalStatus == tlb.AccountStatusFrozen && (runtimeAcc.status == tlb.AccountStatusActive || accountActivated) {
		nextStateHash = nil
	}
	nextAccountCell, nextAccountState, err := buildTransactionAccountCell(computeAcc, finalStatus, finalBalance, nextExtraCurrencies, endLT, prepared.lastPaid, prepared.duePayment, nextCode, nextData, nextLibraries, nextStateHash)
	if err != nil {
		return nil, err
	}

	txCell, err := buildTransactionCell(transactionBuildParams{
		accountAddr: runtimeAcc.addr,
		startLT:     startLT,
		prevTxHash:  runtimeAcc.prevTxHash,
		prevTxLT:    runtimeAcc.prevTxLT,
		now:         now,
		origStatus:  runtimeAcc.status,
		endStatus:   finalStatus,
		inMsg:       msgCell,
		outMsgs:     outMessages,
		totalFees:   totalFees,
		oldHash:     runtimeAcc.originalCell.Hash(),
		newHash:     nextAccountCell.Hash(),
		description: transactionBuildDescriptionParams{
			msg:           &msg,
			creditFirst:   prepared.creditFirst,
			storagePhase:  prepared.storagePhase,
			creditPhase:   prepared.creditPhase,
			computeResult: msgRes,
			computeGas:    gas,
			gasFees:       gasFees,
			actionPhase:   actionPhase,
			bouncePhase:   bouncePhase,
			skipReason:    skipReason,
			msgStateUsed:  msgStateUsed,
			activated:     accountActivated,
			destroyed:     prepared.deleted,
		},
	})
	if err != nil {
		return nil, err
	}

	var tx tlb.Transaction
	if err = tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		return nil, fmt.Errorf("failed to decode built transaction: %w", err)
	}
	tx.Hash = append([]byte(nil), txCell.Hash()...)

	nextShard := &tlb.ShardAccount{
		Account:       nextAccountCell,
		LastTransHash: append([]byte(nil), txCell.Hash()...),
		LastTransLT:   startLT,
	}
	nextShardCell, err := tlb.ToCell(nextShard)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize new shard account: %w", err)
	}

	out.Transaction = &tx
	out.TransactionCell = txCell
	out.AccountState = nextAccountState
	out.AccountCell = nextAccountCell
	out.ShardAccount = nextShard
	out.ShardAccountCell = nextShardCell
	return out, nil
}

func loadTransactionRuntimeAccount(shard *tlb.ShardAccount, fallbackAddr *address.Address, proof *cell.MerkleProofBuilder) (*transactionRuntimeAccount, error) {
	if shard.Account == nil {
		return nil, errors.New("shard account root is nil")
	}

	accountRoot := shard.Account
	if proof != nil {
		accountRoot = proof.Root()
	}

	var acc tlb.AccountState
	if err := tlb.LoadFromCell(&acc, accountRoot.MustBeginParse()); err != nil {
		return nil, fmt.Errorf("failed to decode account state: %w", err)
	}

	out := &transactionRuntimeAccount{
		addr:            fallbackAddr,
		status:          tlb.AccountStatusNonExist,
		storageInfo:     tlb.StorageInfo{StorageExtra: tlb.StorageExtraNone{}},
		balance:         big.NewInt(0),
		prevTxHash:      append([]byte(nil), shard.LastTransHash...),
		prevTxLT:        shard.LastTransLT,
		originalCell:    shard.Account,
		extraCurrencies: nil,
	}
	if !acc.IsValid {
		if out.addr == nil {
			return nil, errors.New("account address is required for non-existing shard account")
		}
		return out, nil
	}

	out.addr = acc.Address
	out.status = acc.Status
	out.storageInfo = acc.StorageInfo
	if out.storageInfo.StorageExtra == nil {
		out.storageInfo.StorageExtra = tlb.StorageExtraNone{}
	}
	out.balance = new(big.Int).Set(acc.Balance.Nano())
	out.extraCurrencies = acc.ExtraCurrencies
	out.storageLT = acc.LastTransactionLT
	out.stateHash = append([]byte(nil), acc.StateHash...)
	storageCell, err := tlb.ToCell(&acc.AccountStorage)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize original account storage: %w", err)
	}
	out.storageCell = storageCell
	if acc.StateInit != nil {
		out.code = acc.StateInit.Code
		out.data = acc.StateInit.Data
		out.libraries = acc.StateInit.Lib
		out.tickTock = acc.StateInit.TickTock
	}
	return out, nil
}

func transactionPrepareInitialPhases(acc *transactionRuntimeAccount, msg *tlb.Message, storageFee, importFee *big.Int, now uint32, limits transactionStorageDueLimits) (*transactionPreparedPhases, error) {
	extraCurrencies, err := transactionCloneExtraCurrencies(acc.extraCurrencies)
	if err != nil {
		return nil, err
	}
	prepared := &transactionPreparedPhases{
		balance:         new(big.Int).Set(acc.balance),
		extraCurrencies: extraCurrencies,
		msgBalance:      transactionZeroCurrencyBalance(),
		creditFirst:     true,
		status:          transactionInitialComputeStatus(acc.status),
		duePayment:      transactionCoinsPtr(transactionCoinsNano(acc.storageInfo.DuePayment)),
		lastPaid:        acc.storageInfo.LastPaid,
	}

	credit := func(amount *big.Int, extra *cell.Dictionary) error {
		merged, err := transactionAddExtraCurrencies(prepared.extraCurrencies, extra)
		if err != nil {
			return err
		}
		prepared.extraCurrencies = merged
		prepared.balance.Add(prepared.balance, amount)
		prepared.creditPhase = &tlb.CreditPhase{
			Credit: tlb.CurrencyCollection{
				Coins:           tlb.FromNanoTON(amount),
				ExtraCurrencies: extra,
			},
		}
		return nil
	}

	switch msg.MsgType {
	case tlb.MsgTypeInternal:
		in := msg.AsInternal()
		prepared.creditFirst = !in.Bounce
		prepared.msgBalance, err = transactionCurrencyFromParts(in.Amount.Nano(), in.ExtraCurrencies)
		if err != nil {
			return nil, err
		}
		if prepared.creditFirst {
			if err = credit(prepared.msgBalance.grams, in.ExtraCurrencies); err != nil {
				return nil, err
			}
			prepared.applyStoragePhase(acc, storageFee, now, limits, true)
		} else {
			prepared.applyStoragePhase(acc, storageFee, now, limits, false)
			if err = credit(prepared.msgBalance.grams, in.ExtraCurrencies); err != nil {
				return nil, err
			}
		}
	case tlb.MsgTypeExternalIn:
		if importFee.Sign() > 0 {
			if prepared.balance.Cmp(importFee) < 0 {
				return nil, errors.New("external import fees exceed account balance")
			}
			prepared.balance.Sub(prepared.balance, importFee)
		}
		prepared.applyStoragePhase(acc, storageFee, now, limits, false)
	default:
		return nil, fmt.Errorf("unsupported input message type %s", msg.MsgType)
	}

	return prepared, nil
}

func (p *transactionPreparedPhases) applyStoragePhase(acc *transactionRuntimeAccount, storageFee *big.Int, now uint32, limits transactionStorageDueLimits, adjustMsgValue bool) {
	toPay := transactionBigOrZero(storageFee)
	collected := big.NewInt(0)
	due := big.NewInt(0)
	statusChange := tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}

	p.duePayment = transactionCoinsPtr(transactionCoinsNano(acc.storageInfo.DuePayment))
	p.lastPaid = now
	if toPay.Sign() > 0 {
		if toPay.Cmp(p.balance) <= 0 {
			collected.Set(toPay)
			p.balance.Sub(p.balance, toPay)
			p.duePayment = nil
		} else {
			collected.Set(p.balance)
			due.Sub(toPay, p.balance)
			p.balance.SetInt64(0)

			switch p.status {
			case tlb.AccountStatusUninit, tlb.AccountStatusFrozen, tlb.AccountStatusNonExist:
				if due.Cmp(limits.deleteDue) > 0 && transactionExtraDictIsEmpty(acc.extraCurrencies) {
					p.deleted = true
					p.status = tlb.AccountStatusNonExist
					statusChange.Type = tlb.AccStatusChangeDeleted
				}
			case tlb.AccountStatusActive:
				if due.Cmp(limits.freezeDue) > 0 {
					p.status = tlb.AccountStatusFrozen
					statusChange.Type = tlb.AccStatusChangeFrozen
				}
			}
			p.duePayment = transactionCoinsPtr(due)
		}
	}

	if adjustMsgValue && p.msgBalance.grams.Cmp(p.balance) > 0 {
		p.msgBalance.grams.Set(p.balance)
	}

	p.storagePhase = &tlb.StoragePhase{
		StorageFeesCollected: tlb.FromNanoTON(collected),
		StorageFeesDue:       transactionCoinsPtr(due),
		StatusChange:         statusChange,
	}
}

func (tvm *TVM) executeTransactionMessage(acc *transactionRuntimeAccount, msgCell *cell.Cell, msg *tlb.Message, cfg TransactionEmulationConfig, gas vm.Gas, msgBalance *big.Int, proof *cell.MerkleProofBuilder) (*MessageExecutionResult, error) {
	body := messageBodyCell(msg.Msg.Payload())
	stack := vm.NewStack()
	balance := new(big.Int).Set(cfg.Balance)

	switch msg.MsgType {
	case tlb.MsgTypeExternalIn:
		if err := stack.PushInt(balance); err != nil {
			return nil, err
		}
		if err := stack.PushInt(big.NewInt(0)); err != nil {
			return nil, err
		}
		if err := stack.PushCell(msgCell); err != nil {
			return nil, err
		}
		if err := stack.PushSlice(body.MustBeginParse()); err != nil {
			return nil, err
		}
		if err := stack.PushInt(big.NewInt(-1)); err != nil {
			return nil, err
		}
	case tlb.MsgTypeInternal:
		if err := stack.PushInt(balance); err != nil {
			return nil, err
		}
		if err := stack.PushInt(transactionBigOrZero(msgBalance)); err != nil {
			return nil, err
		}
		if err := stack.PushCell(msgCell); err != nil {
			return nil, err
		}
		if err := stack.PushSlice(body.MustBeginParse()); err != nil {
			return nil, err
		}
		if err := stack.PushInt(big.NewInt(0)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported input message type %s", msg.MsgType)
	}

	c7, err := buildMessageEmulationC7(acc.addr, acc.code, cfg, balance)
	if err != nil {
		return nil, err
	}

	libraries := append([]*cell.Cell(nil), cfg.Libraries...)
	if acc.libraries != nil && acc.libraries.AsCell() != nil {
		libraries = append(libraries, acc.libraries.AsCell())
	}

	res, err := tvm.executeMessageEmulation(acc.code, acc.data, c7, gas, stack, cfg.StopOnAccept, proof, libraries...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func transactionMessageGas(cfg TransactionEmulationConfig, blockchainCfg tlb.BlockchainConfig, addr *address.Address, balance, msgBalance *big.Int, msgType tlb.MsgType) vm.Gas {
	if transactionGasConfigured(cfg.Gas) {
		return cfg.Gas
	}

	prices, err := blockchainCfg.GetGasPrices(transactionIsMasterchain(addr))
	if err == nil && prices != nil && prices.GasLimit > 0 {
		gasMax := transactionGasBoughtFor(prices, balance)
		gasLimit := uint64(0)
		gasCredit := uint64(0)
		if msgType == tlb.MsgTypeExternalIn {
			gasCredit = min(prices.GasCredit, gasMax)
		} else {
			gasLimit = min(transactionGasBoughtFor(prices, msgBalance), gasMax)
		}
		return transactionGasFromLimits(gasMax, gasLimit, gasCredit)
	}

	if msgType == tlb.MsgTypeInternal {
		if msgBalance == nil || !msgBalance.IsUint64() {
			return defaultInternalMessageGas(vm.Gas{}, 0)
		}
		return defaultInternalMessageGas(vm.Gas{}, msgBalance.Uint64())
	}
	return defaultExternalMessageGas(vm.Gas{})
}

func transactionGasFromLimits(max, limit, credit uint64) vm.Gas {
	maxGas := transactionGasInt(max)
	limitGas := transactionGasInt(limit)
	creditGas := transactionGasInt(credit)
	return vm.Gas{
		Max:       maxGas,
		Limit:     limitGas,
		Credit:    creditGas,
		Base:      limitGas + creditGas,
		Remaining: limitGas + creditGas,
	}
}

func transactionGasInt(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

func transactionGasBoughtFor(prices *tlb.ConfigGasLimitsPrices, nanograms *big.Int) uint64 {
	if prices == nil || nanograms == nil || nanograms.Sign() < 0 {
		return 0
	}

	threshold := transactionMaxGasThreshold(prices)
	if nanograms.Cmp(threshold) >= 0 {
		return prices.GasLimit
	}
	if nanograms.Cmp(new(big.Int).SetUint64(prices.FlatGasPrice)) < 0 {
		return 0
	}
	if prices.GasPrice == 0 {
		return prices.GasLimit
	}

	remaining := new(big.Int).Sub(new(big.Int).Set(nanograms), new(big.Int).SetUint64(prices.FlatGasPrice))
	remaining.Lsh(remaining, 16)
	remaining.Div(remaining, new(big.Int).SetUint64(prices.GasPrice))
	remaining.Add(remaining, new(big.Int).SetUint64(prices.FlatGasLimit))
	if !remaining.IsUint64() || remaining.Uint64() > prices.GasLimit {
		return prices.GasLimit
	}
	return remaining.Uint64()
}

func transactionMaxGasThreshold(prices *tlb.ConfigGasLimitsPrices) *big.Int {
	if prices == nil || prices.GasLimit <= prices.FlatGasLimit {
		return new(big.Int).SetUint64(transactionGasFlatPrice(prices))
	}

	units := new(big.Int).SetUint64(prices.GasLimit - prices.FlatGasLimit)
	total := new(big.Int).Mul(new(big.Int).SetUint64(prices.GasPrice), units)
	total = transactionCeilShiftRight(total, 16)
	return total.Add(total, new(big.Int).SetUint64(prices.FlatGasPrice))
}

func transactionGasFlatPrice(prices *tlb.ConfigGasLimitsPrices) uint64 {
	if prices == nil {
		return 0
	}
	return prices.FlatGasPrice
}

func transactionCeilShiftRight(x *big.Int, bits uint) *big.Int {
	if x == nil || x.Sign() == 0 {
		return big.NewInt(0)
	}

	add := new(big.Int).Lsh(big.NewInt(1), bits)
	add.Sub(add, big.NewInt(1))
	return new(big.Int).Rsh(new(big.Int).Add(x, add), bits)
}

func transactionGasConfigured(g vm.Gas) bool {
	return g.Max != 0 || g.Limit != 0 || g.Credit != 0
}

func transactionStartLT(storageLT uint64, logicalTime int64, msg *tlb.Message) uint64 {
	var start uint64
	if logicalTime > 0 {
		start = uint64(logicalTime)
	}

	if storageLT > start {
		start = storageLT
	}

	if msg != nil && msg.MsgType == tlb.MsgTypeInternal {
		if createdLT := msg.AsInternal().CreatedLT; createdLT >= start {
			start = createdLT + 1
		}
	}

	if start == 0 {
		start = 1
	}
	return start
}

type transactionActionApplyResult struct {
	outMsgs         []*cell.Cell
	phase           *tlb.ActionPhase
	nextCode        *cell.Cell
	nextLibraries   *cell.Dictionary
	extraCurrencies *cell.Dictionary
	endLT           uint64
	balance         *big.Int
	actionFees      *big.Int
	actionFine      *big.Int
	bounce          bool
	deleteAccount   bool
}

type transactionSendActionResult struct {
	msgCell         *cell.Cell
	usage           transactionUsage
	debit           *transactionCurrencyBalance
	totalFwdFees    *big.Int
	totalActionFees *big.Int
	actionFine      *big.Int
	resultCode      int32
	clearMsgBalance bool
	skipped         bool
	ignored         bool
	invalid         bool
	bounceOnFail    bool
	deleteAccount   bool
}

type transactionReserveActionResult struct {
	resultCode   int32
	bounceOnFail bool
}

type transactionChangeLibraryActionResult struct {
	nextLibraries *cell.Dictionary
	resultCode    int32
	bounceOnFail  bool
}

type transactionActionEntry struct {
	action  any
	skipped bool
}

type transactionActionLoadResult struct {
	actions        []transactionActionEntry
	totalActions   uint16
	skippedActions uint16
	resultCode     int32
	resultArg      *int32
	bounce         bool
}

func transactionApplyActions(acc *transactionRuntimeAccount, res *MessageExecutionResult, startLT uint64, now uint32, cfg tlb.BlockchainConfig, balanceAfterGas *big.Int, extraCurrencies *cell.Dictionary, msgBalance *transactionCurrencyBalance, gasFees *big.Int) (*transactionActionApplyResult, error) {
	computeSuccess := vm.IsSuccessExitCode(res.ExitCode)
	endLT := startLT + 1
	out := &transactionActionApplyResult{
		nextCode:        acc.code,
		nextLibraries:   acc.libraries,
		extraCurrencies: extraCurrencies,
		endLT:           endLT,
		balance:         transactionBigOrZero(balanceAfterGas),
		actionFees:      big.NewInt(0),
		actionFine:      big.NewInt(0),
	}
	if !computeSuccess {
		return out, nil
	}

	actionsRoot := res.Actions
	if actionsRoot == nil {
		actionsRoot = cell.BeginCell().EndCell()
	}

	loadedActions, err := transactionLoadActions(actionsRoot)
	if err != nil {
		return nil, err
	}
	actionPhase := &tlb.ActionPhase{
		Success:        false,
		Valid:          loadedActions.resultCode == 0,
		NoFunds:        false,
		StatusChange:   tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged},
		ResultCode:     -1,
		ActionListHash: append([]byte(nil), actionsRoot.Hash()...),
		TotalActions:   loadedActions.totalActions,
		SkippedActions: loadedActions.skippedActions,
		TotalMsgSize: tlb.StorageUsedShort{
			Cells: big.NewInt(0),
			Bits:  big.NewInt(0),
		},
	}
	out.phase = actionPhase
	if loadedActions.resultCode != 0 {
		actionPhase.ResultCode = loadedActions.resultCode
		actionPhase.ResultArg = loadedActions.resultArg
		out.bounce = loadedActions.bounce
		return out, nil
	}

	actions := loadedActions.actions
	outMsgs := make([]*cell.Cell, 0, len(actions))
	totalUsage := transactionUsage{}
	totalFwdFees := big.NewInt(0)
	totalActionFees := big.NewInt(0)
	actionFine := big.NewInt(0)
	specActions := uint16(0)
	nextCode := acc.code
	nextLibraries := acc.libraries
	remainingBalance, err := transactionCurrencyFromParts(balanceAfterGas, extraCurrencies)
	if err != nil {
		return nil, err
	}
	msgBalanceRemaining := msgBalance.copy()
	reservedBalance := transactionZeroCurrencyBalance()
	originalBalance := remainingBalance.copy()
	originalBalance.grams.Add(originalBalance.grams, transactionBigOrZero(gasFees))
	if !originalBalance.sub(msgBalance) {
		originalBalance = remainingBalance.copy()
	}

	failAction := func(resultCode int32, idx int, bounceOnFail bool, noFunds bool, valid bool) {
		actionPhase.ResultCode = resultCode
		actionPhase.Valid = valid
		actionPhase.NoFunds = noFunds
		actionPhase.ResultArg = transactionActionResultArg(idx)
		actionPhase.SpecActions = specActions
		actionPhase.MessagesCreated = uint16(len(outMsgs))
		actionPhase.TotalFwdFees = transactionCoinsPtr(totalFwdFees)
		if actionFine.Sign() > 0 {
			actionPhase.TotalActionFees = transactionCoinsPtr(actionFine)
		} else {
			actionPhase.TotalActionFees = nil
		}
		actionPhase.TotalMsgSize = tlb.StorageUsedShort{
			Cells: new(big.Int).SetUint64(totalUsage.cells),
			Bits:  new(big.Int).SetUint64(totalUsage.bits),
		}
		if bounceOnFail {
			out.bounce = true
		}
		out.nextLibraries = nextLibraries
		out.actionFine = new(big.Int).Set(actionFine)
		out.actionFees = new(big.Int).Set(actionFine)
		out.balance = new(big.Int).Sub(transactionBigOrZero(balanceAfterGas), actionFine)
		if out.balance.Sign() < 0 {
			out.balance.SetInt64(0)
		}
	}

	for i, action := range actions {
		if action.skipped {
			continue
		}
		switch act := action.action.(type) {
		case tlb.ActionSendMsg:
			sendRes, err := transactionProcessSendAction(acc, act, startLT+1+uint64(len(outMsgs)), now, cfg, remainingBalance, msgBalanceRemaining, gasFees)
			if err != nil {
				return nil, err
			}
			if sendRes.actionFine.Sign() > 0 {
				fine := transactionMinBig(sendRes.actionFine, remainingBalance.grams)
				remainingBalance.grams.Sub(remainingBalance.grams, fine)
				actionFine.Add(actionFine, fine)
				totalActionFees.Add(totalActionFees, fine)
			}
			if sendRes.skipped {
				actionPhase.SkippedActions++
				continue
			}
			if sendRes.ignored {
				continue
			}
			if sendRes.resultCode != 0 {
				failAction(sendRes.resultCode, i, sendRes.bounceOnFail, sendRes.resultCode == 37 || sendRes.resultCode == 38, !sendRes.invalid)
				return out, nil
			}
			if !remainingBalance.sub(sendRes.debit) {
				failAction(37, i, sendRes.bounceOnFail, true, true)
				return out, nil
			}
			if sendRes.clearMsgBalance {
				msgBalanceRemaining = transactionZeroCurrencyBalance()
			}
			outMsgs = append(outMsgs, sendRes.msgCell)
			totalUsage = transactionAddUsage(totalUsage, sendRes.usage)
			totalFwdFees.Add(totalFwdFees, sendRes.totalFwdFees)
			totalActionFees.Add(totalActionFees, sendRes.totalActionFees)
			if sendRes.deleteAccount {
				out.deleteAccount = true
			}
		case tlb.ActionSetCode:
			specActions++
			nextCode = act.NewCode
		case tlb.ActionReserveCurrency:
			reserveRes, err := transactionProcessReserveAction(act, originalBalance, remainingBalance, reservedBalance)
			if err != nil {
				return nil, err
			}
			if reserveRes.resultCode != 0 {
				failAction(reserveRes.resultCode, i, reserveRes.bounceOnFail, reserveRes.resultCode == 37 || reserveRes.resultCode == 38, true)
				return out, nil
			}
			specActions++
		case tlb.ActionChangeLibrary:
			libRes, err := transactionProcessChangeLibraryAction(act, nextLibraries, cfg)
			if err != nil {
				return nil, err
			}
			if libRes.nextLibraries != nil {
				nextLibraries = libRes.nextLibraries
			}
			if libRes.resultCode != 0 {
				failAction(libRes.resultCode, i, libRes.bounceOnFail, false, true)
				out.nextLibraries = nextLibraries
				return out, nil
			}
			specActions++
		default:
			failAction(34, i, false, false, false)
			return out, nil
		}
	}

	remainingBalance.add(reservedBalance)

	stateLimitExceeded, err := transactionAccountStateExceedsLimits(acc, nextCode, res.Data, nextLibraries, cfg)
	if err != nil {
		return nil, err
	}
	if stateLimitExceeded {
		actionPhase.Valid = true
		actionPhase.ResultCode = 50
		actionPhase.ResultArg = nil
		actionPhase.SpecActions = specActions
		actionPhase.MessagesCreated = uint16(len(outMsgs))
		actionPhase.TotalFwdFees = transactionCoinsPtr(totalFwdFees)
		actionPhase.TotalActionFees = transactionCoinsPtr(actionFine)
		actionPhase.TotalMsgSize = tlb.StorageUsedShort{
			Cells: new(big.Int).SetUint64(totalUsage.cells),
			Bits:  new(big.Int).SetUint64(totalUsage.bits),
		}
		out.bounce = true
		out.actionFine = actionFine
		out.actionFees = actionFine
		out.balance = new(big.Int).Sub(transactionBigOrZero(balanceAfterGas), actionFine)
		if out.balance.Sign() < 0 {
			out.balance.SetInt64(0)
		}
		return out, nil
	}

	endLT = startLT + 1 + uint64(len(outMsgs))
	actionPhase.Success = true
	actionPhase.Valid = true
	actionPhase.NoFunds = false
	actionPhase.ResultCode = 0
	actionPhase.ResultArg = nil
	actionPhase.TotalActions = uint16(len(actions))
	actionPhase.SpecActions = specActions
	actionPhase.MessagesCreated = uint16(len(outMsgs))
	if out.deleteAccount {
		actionPhase.StatusChange = tlb.AccStatusChange{Type: tlb.AccStatusChangeDeleted}
	}
	actionPhase.TotalFwdFees = transactionCoinsPtr(totalFwdFees)
	actionPhase.TotalActionFees = transactionCoinsPtr(totalActionFees)
	actionPhase.TotalMsgSize = tlb.StorageUsedShort{
		Cells: new(big.Int).SetUint64(totalUsage.cells),
		Bits:  new(big.Int).SetUint64(totalUsage.bits),
	}
	extraDict, err := remainingBalance.extraDict()
	if err != nil {
		return nil, err
	}
	out.outMsgs = outMsgs
	out.nextCode = nextCode
	out.nextLibraries = nextLibraries
	out.extraCurrencies = extraDict
	out.endLT = endLT
	out.balance = remainingBalance.grams
	out.actionFees = totalActionFees
	out.actionFine = actionFine
	return out, nil
}

func transactionLoadActions(root *cell.Cell) (*transactionActionLoadResult, error) {
	out := &transactionActionLoadResult{}
	if root == nil {
		return out, nil
	}
	if transactionCellIsEmpty(root) {
		return out, nil
	}

	var nodes []*cell.Cell
	for cur := root; cur != nil && !transactionCellIsEmpty(cur); {
		if cur.IsSpecial() {
			out.resultCode = 32
			arg := int32(len(nodes))
			out.resultArg = &arg
			return out, nil
		}
		sl := cur.MustBeginParse()
		if sl.RefsNum() == 0 {
			out.resultCode = 32
			arg := int32(len(nodes))
			out.resultArg = &arg
			return out, nil
		}
		prev, err := sl.LoadRefCell()
		if err != nil {
			out.resultCode = 32
			arg := int32(len(nodes))
			out.resultArg = &arg
			return out, nil
		}
		nodes = append(nodes, cur)
		if len(nodes) > 255 {
			out.resultCode = 33
			arg := int32(len(nodes))
			out.resultArg = &arg
			return out, nil
		}
		cur = prev
	}

	out.totalActions = uint16(len(nodes))
	actions := make([]transactionActionEntry, 0, len(nodes))
	for i := len(nodes) - 1; i >= 0; i-- {
		node := nodes[i]
		var list tlb.OutList
		if err := tlb.LoadFromCell(&list, node.MustBeginParse()); err != nil {
			mode, isSend := transactionMalformedSendMode(node)
			if isSend {
				if mode&2 != 0 {
					out.skippedActions++
					actions = append(actions, transactionActionEntry{skipped: true})
					continue
				}
				if mode&16 != 0 {
					out.bounce = true
				}
			}
			out.resultCode = 34
			out.resultArg = transactionActionResultArg(len(actions))
			return out, nil
		}
		actions = append(actions, transactionActionEntry{action: list.Out})
	}
	out.actions = actions
	return out, nil
}

func transactionCellIsEmpty(c *cell.Cell) bool {
	return c == nil || (c.BitsSize() == 0 && c.RefsNum() == 0)
}

func transactionMalformedSendMode(node *cell.Cell) (uint8, bool) {
	if node == nil || node.IsSpecial() {
		return 0, false
	}
	sl := node.MustBeginParse()
	if sl.RefsNum() == 0 {
		return 0, false
	}
	if _, err := sl.LoadRefCell(); err != nil {
		return 0, false
	}
	if sl.BitsLeft() < 40 {
		return 0, false
	}
	tag, err := sl.LoadUInt(32)
	if err != nil || tag != 0x0ec3c86d {
		return 0, false
	}
	mode, err := sl.LoadUInt(8)
	if err != nil {
		return 0, false
	}
	return uint8(mode), true
}

func transactionProcessSendAction(acc *transactionRuntimeAccount, act tlb.ActionSendMsg, createdLT uint64, now uint32, cfg tlb.BlockchainConfig, remainingBalance, msgBalanceRemaining *transactionCurrencyBalance, gasFees *big.Int) (*transactionSendActionResult, error) {
	out := &transactionSendActionResult{
		debit:           transactionZeroCurrencyBalance(),
		totalFwdFees:    big.NewInt(0),
		totalActionFees: big.NewInt(0),
		actionFine:      big.NewInt(0),
	}

	mode := act.Mode
	if mode&16 != 0 {
		mode &^= 16
		out.bounceOnFail = true
	}

	if transactionSendModeInvalid(mode) {
		return transactionSendIgnored(out, mode), nil
	}

	var suggestedMsg tlb.Message
	if err := tlb.LoadFromCell(&suggestedMsg, act.Msg.MustBeginParse()); err != nil {
		if mode&2 != 0 {
			return transactionSendIgnored(out, mode), nil
		}
		out.resultCode = 34
		out.invalid = true
		return out, nil
	}
	switch suggestedMsg.MsgType {
	case tlb.MsgTypeInternal:
		intMsg := suggestedMsg.AsInternal()
		if !transactionOutboundSourceValid(intMsg.SrcAddr, acc.addr) {
			return transactionSendInvalidSource(out, mode), nil
		}
		if !transactionOutboundInternalDestTypeValid(intMsg.DstAddr) {
			out.resultCode = 34
			out.invalid = true
			return out, nil
		}
		if !transactionOutboundInternalDestValid(intMsg.DstAddr, cfg) {
			return transactionSendInvalidDestination(out, mode), nil
		}
	case tlb.MsgTypeExternalOut:
		if !transactionOutboundSourceValid(suggestedMsg.AsExternalOut().SrcAddr, acc.addr) {
			return transactionSendInvalidSource(out, mode), nil
		}
	default:
		out.resultCode = 34
		out.invalid = true
		return out, nil
	}

	msgCell, err := transactionNormalizeOutboundMessage(act.Msg, acc.addr, createdLT, now, cfg)
	if err != nil {
		if errors.Is(err, errTransactionInvalidDestination) {
			return transactionSendInvalidDestination(out, mode), nil
		}
		if mode&2 != 0 {
			return transactionSendIgnored(out, mode), nil
		}
		out.resultCode = 34
		return out, nil
	}

	var msg tlb.Message
	if err = tlb.LoadFromCell(&msg, msgCell.MustBeginParse()); err != nil {
		if mode&2 != 0 {
			return transactionSendIgnored(out, mode), nil
		}
		out.resultCode = 34
		return out, nil
	}

	switch msg.MsgType {
	case tlb.MsgTypeExternalOut:
		extMsg := msg.AsExternalOut()
		if extMsg.DstAddr != nil && extMsg.DstAddr.Type() != address.NoneAddress && extMsg.DstAddr.Type() != address.ExtAddress {
			out.resultCode = 34
			out.invalid = true
			return out, nil
		}
		if mode&^uint8(3) != 0 {
			out.resultCode = 34
			return out, nil
		}
		if sizeCode, fine := transactionCheckOutboundMessageSize(cfg, acc.addr, msg.Msg.DestAddr(), msgCell, remainingBalance.grams); sizeCode != 0 {
			out.actionFine = fine
			return transactionSendResultCode(out, mode, sizeCode), nil
		}
		fwdFee := transactionComputeForwardFeeForMessage(cfg, acc.addr, msg.Msg.DestAddr(), msgCell)
		if remainingBalance.grams.Cmp(fwdFee) < 0 {
			out.actionFine = transactionComputeActionFine(cfg, acc.addr, msg.Msg.DestAddr(), msgCell, remainingBalance.grams)
			return transactionSendResultCode(out, mode, 37), nil
		}
		out.msgCell = msgCell
		out.usage = transactionCollectUsage(msgCell)
		out.debit.grams = fwdFee
		out.totalFwdFees = fwdFee
		out.totalActionFees = fwdFee
		return out, nil
	case tlb.MsgTypeInternal:
		intMsg := *msg.AsInternal()
		intMsg.IHRDisabled = true
		extraFlags := transactionBigOrZero(intMsg.IHRFee.Nano())
		if !extraFlags.IsUint64() || extraFlags.Uint64()&^uint64(3) != 0 {
			return transactionSendResultCode(out, mode, 45), nil
		}

		req, err := transactionCurrencyFromParts(intMsg.Amount.Nano(), intMsg.ExtraCurrencies)
		if err != nil {
			return transactionSendResultCode(out, mode, 37), nil
		}
		req.removeZeroExtra()
		if transactionExtraCount(req.extra) > transactionGetSizeLimits(cfg).maxMsgExtraCurrencies {
			return transactionSendResultCode(out, mode, 44), nil
		}
		if sizeCode, fine := transactionCheckOutboundMessageSize(cfg, acc.addr, intMsg.DstAddr, msgCell, remainingBalance.grams); sizeCode != 0 {
			out.actionFine = fine
			return transactionSendResultCode(out, mode, sizeCode), nil
		}

		computedFwdFee := transactionComputeForwardFeeForMessage(cfg, acc.addr, intMsg.DstAddr, msgCell)
		fwdFee := computedFwdFee
		ihrFee := transactionComputeIHRFee(cfg, acc.addr, intMsg.DstAddr, computedFwdFee, intMsg.IHRDisabled)
		totalFees := new(big.Int).Add(fwdFee, ihrFee)
		collectedFwdFee := transactionFirstPartForwardFee(cfg, acc.addr, intMsg.DstAddr, fwdFee)
		remainingFwdFee := new(big.Int).Sub(fwdFee, collectedFwdFee)

		clearMsgBalance := false
		sendMode := mode
		if mode&128 != 0 {
			req.grams = new(big.Int).Set(remainingBalance.grams)
			mode &^= 1
			clearMsgBalance = true
		} else if mode&64 != 0 {
			req.grams.Add(req.grams, msgBalanceRemaining.grams)
			clearMsgBalance = true
			if mode&1 == 0 {
				req.grams.Sub(req.grams, gasFees)
				req.grams.Sub(req.grams, out.actionFine)
				if req.grams.Sign() < 0 {
					out.actionFine = transactionComputeActionFine(cfg, acc.addr, intMsg.DstAddr, msgCell, remainingBalance.grams)
					return transactionSendResultCode(out, mode, 37), nil
				}
			}
		}

		debit := req.copy()
		msgAmount := new(big.Int).Set(req.grams)
		if mode&1 != 0 {
			debit.grams.Add(debit.grams, totalFees)
		} else {
			if msgAmount.Cmp(totalFees) < 0 {
				out.actionFine = transactionComputeActionFine(cfg, acc.addr, intMsg.DstAddr, msgCell, remainingBalance.grams)
				return transactionSendResultCode(out, mode, 37), nil
			}
			msgAmount.Sub(msgAmount, totalFees)
		}
		if remainingBalance.grams.Cmp(debit.grams) < 0 {
			out.actionFine = transactionComputeActionFine(cfg, acc.addr, intMsg.DstAddr, msgCell, remainingBalance.grams)
			return transactionSendResultCode(out, mode, 37), nil
		}
		if !remainingBalance.hasExtra(debit.extra) {
			out.actionFine = transactionComputeActionFine(cfg, acc.addr, intMsg.DstAddr, msgCell, remainingBalance.grams)
			return transactionSendResultCode(out, mode, 38), nil
		}
		if transactionExtraCount(req.extra) > transactionGetSizeLimits(cfg).maxMsgExtraCurrencies {
			return transactionSendResultCode(out, mode, 44), nil
		}

		intMsg.Amount = tlb.FromNanoTON(msgAmount)
		intMsg.ExtraCurrencies, err = req.extraDict()
		if err != nil {
			return nil, err
		}
		intMsg.FwdFee = tlb.FromNanoTON(remainingFwdFee)
		intMsg.IHRFee = tlb.FromNanoTON(extraFlags)
		msgCell, err = tlb.ToCell(&intMsg)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize outbound internal message: %w", err)
		}
		out.msgCell = msgCell
		out.usage = transactionCollectUsage(msgCell)
		out.debit = debit
		out.totalFwdFees = totalFees
		out.totalActionFees = collectedFwdFee
		out.clearMsgBalance = clearMsgBalance
		if sendMode&0xA0 == 0xA0 {
			out.deleteAccount = true
		}
		return out, nil
	default:
		out.resultCode = 34
		return out, nil
	}
}

func transactionSendModeInvalid(mode uint8) bool {
	return mode&^uint8(0xE3) != 0 || mode&0xC0 == 0xC0
}

func transactionOutboundSourceValid(src, account *address.Address) bool {
	if src == nil || src.Type() == address.NoneAddress {
		return true
	}
	if src.Type() != address.StdAddress || account == nil || account.Type() != address.StdAddress {
		return false
	}
	return src.Equals(account)
}

func transactionOutboundInternalDestValid(dst *address.Address, cfg tlb.BlockchainConfig) bool {
	_, ok := transactionValidateAndNormalizeInternalDestAddr(dst, cfg)
	return ok
}

func transactionSendInvalidSource(out *transactionSendActionResult, mode uint8) *transactionSendActionResult {
	if mode&2 != 0 {
		out.skipped = true
		return out
	}
	out.resultCode = 35
	return out
}

func transactionSendInvalidDestination(out *transactionSendActionResult, mode uint8) *transactionSendActionResult {
	if mode&2 != 0 {
		out.skipped = true
		return out
	}
	out.resultCode = 36
	return out
}

func transactionSendResultCode(out *transactionSendActionResult, mode uint8, code int32) *transactionSendActionResult {
	if mode&2 != 0 {
		out.skipped = true
		return out
	}
	out.resultCode = code
	return out
}

func transactionSendIgnored(out *transactionSendActionResult, mode uint8) *transactionSendActionResult {
	if mode&2 != 0 {
		out.skipped = true
		return out
	}
	out.resultCode = 34
	return out
}

func transactionProcessReserveAction(act tlb.ActionReserveCurrency, originalBalance, remainingBalance, reservedBalance *transactionCurrencyBalance) (*transactionReserveActionResult, error) {
	out := &transactionReserveActionResult{}
	mode := act.Mode
	if mode&16 != 0 {
		mode &^= 16
		out.bounceOnFail = true
	}
	if mode&^uint8(15) != 0 {
		out.resultCode = 34
		return out, nil
	}

	reserve, err := transactionCurrencyFromCollection(act.Currency)
	if err != nil {
		out.resultCode = 34
		return out, nil
	}
	if !reserve.extraEmpty() {
		out.resultCode = 34
		return out, nil
	}

	if mode&4 != 0 {
		if mode&8 != 0 {
			next := originalBalance.copy()
			if !next.sub(reserve) {
				out.resultCode = 34
				return out, nil
			}
			reserve = next
		} else {
			reserve.add(originalBalance)
		}
	} else if mode&8 != 0 {
		out.resultCode = 34
		return out, nil
	}

	if reserve.grams.Sign() < 0 {
		out.resultCode = 34
		return out, nil
	}
	if mode&2 != 0 {
		reserve.clamp(remainingBalance)
	}
	if reserve.grams.Cmp(remainingBalance.grams) > 0 {
		out.resultCode = 37
		return out, nil
	}
	if !remainingBalance.hasExtra(reserve.extra) {
		out.resultCode = 38
		return out, nil
	}

	newBalance := remainingBalance.copy()
	if !newBalance.sub(reserve) {
		out.resultCode = 38
		return out, nil
	}
	if mode&1 != 0 {
		newBalance.grams, reserve.grams = reserve.grams, newBalance.grams
	}

	*remainingBalance = *newBalance
	reservedBalance.add(reserve)
	return out, nil
}

func transactionProcessChangeLibraryAction(act tlb.ActionChangeLibrary, current *cell.Dictionary, cfg tlb.BlockchainConfig) (*transactionChangeLibraryActionResult, error) {
	out := &transactionChangeLibraryActionResult{}
	mode := act.Mode
	if mode&16 != 0 {
		mode &^= 16
		out.bounceOnFail = true
	}
	if mode > 2 {
		out.resultCode = 34
		return out, nil
	}

	libs := cell.NewDict(256)
	if current != nil && !current.IsEmpty() {
		libs = current.Copy()
	}

	var libHash []byte
	var libRef *cell.Cell
	switch ref := act.LibRef.(type) {
	case tlb.LibRefHash:
		libHash = transactionNormalizeBits256(ref.LibHash)
	case tlb.LibRefRef:
		libRef = ref.Library
		if libRef != nil {
			libHash = libRef.Hash()
		}
	default:
		out.resultCode = 34
		return out, nil
	}
	if len(libHash) != 32 {
		out.resultCode = 34
		return out, nil
	}

	key := cell.BeginCell().MustStoreSlice(libHash, 256).EndCell()
	if mode == 0 {
		if err := libs.Delete(key); err != nil {
			out.resultCode = 42
			return out, nil
		}
		out.nextLibraries = libs
		return out, nil
	}

	if existing, err := libs.LoadValue(key); err == nil && existing != nil {
		isPublic, loadErr := existing.LoadBoolBit()
		existingRef, refErr := existing.LoadRefCell()
		if loadErr == nil && refErr == nil && existingRef != nil && bytes.Equal(existingRef.Hash(), libHash) {
			libRef = existingRef
			if isPublic == (mode == 2) {
				out.nextLibraries = libs
				return out, nil
			}
		}
	}
	if libRef == nil {
		out.resultCode = 41
		return out, nil
	}

	usage := transactionCollectUsage(libRef)
	if usage.cells > transactionGetSizeLimits(cfg).maxLibraryCells || transactionMaxMerkleDepth(libRef) > 2 {
		out.resultCode = 43
		return out, nil
	}
	value := cell.BeginCell().MustStoreBoolBit(mode == 2).MustStoreRef(libRef).EndCell()
	if err := libs.Set(key, value); err != nil {
		out.resultCode = 42
		return out, nil
	}
	out.nextLibraries = libs
	return out, nil
}

func transactionActionResultArg(i int) *int32 {
	if i == 0 {
		return nil
	}
	arg := int32(i)
	return &arg
}

var errTransactionInvalidDestination = errors.New("invalid outbound destination address")

func transactionNormalizeOutboundMessage(msgCell *cell.Cell, srcAddr *address.Address, createdLT uint64, now uint32, cfg tlb.BlockchainConfig) (*cell.Cell, error) {
	if msgCell == nil {
		return nil, errors.New("outbound message cell is nil")
	}

	var msg tlb.Message
	if err := tlb.LoadFromCell(&msg, msgCell.MustBeginParse()); err != nil {
		return nil, fmt.Errorf("failed to decode outbound message: %w", err)
	}

	switch msg.MsgType {
	case tlb.MsgTypeInternal:
		out := *msg.AsInternal()
		out.SrcAddr = srcAddr
		dst, ok := transactionValidateAndNormalizeInternalDestAddr(out.DstAddr, cfg)
		if !ok {
			return nil, errTransactionInvalidDestination
		}
		out.DstAddr = dst
		out.CreatedLT = createdLT
		out.CreatedAt = now
		return tlb.ToCell(&out)
	case tlb.MsgTypeExternalOut:
		out := *msg.AsExternalOut()
		out.SrcAddr = srcAddr
		out.CreatedLT = createdLT
		out.CreatedAt = now
		return tlb.ToCell(&out)
	default:
		return nil, fmt.Errorf("unsupported outbound message type %s", msg.MsgType)
	}
}

func transactionValidateAndNormalizeInternalDestAddr(addr *address.Address, cfg tlb.BlockchainConfig) (*address.Address, bool) {
	if addr == nil {
		return nil, false
	}
	if addr.Type() != address.StdAddress && addr.Type() != address.VarAddress {
		return nil, false
	}

	addrLen := addr.BitsLen()
	if addr.Workchain() == address.MasterchainID {
		if addrLen != 256 {
			return nil, false
		}
	} else if info, found, enforce := transactionGetWorkchainInfo(cfg, addr.Workchain()); enforce {
		if !found || !info.acceptMsgs || !info.validAddrLen(addrLen) {
			return nil, false
		}
	}

	if addr.Anycast() != nil {
		return nil, false
	}

	return transactionNormalizeInternalDestAddr(addr), true
}

func transactionOutboundInternalDestTypeValid(addr *address.Address) bool {
	return addr != nil && (addr.Type() == address.StdAddress || addr.Type() == address.VarAddress)
}

func transactionNormalizeInternalDestAddr(addr *address.Address) *address.Address {
	if addr == nil || addr.Type() != address.VarAddress || addr.BitsLen() != 256 {
		return addr
	}
	if addr.Workchain() < -128 || addr.Workchain() >= 128 {
		return addr
	}
	return address.NewAddress(0, byte(int8(addr.Workchain())), append([]byte(nil), addr.Data()...)).WithAnycast(addr.Anycast())
}

type transactionWorkchainInfo struct {
	acceptMsgs  bool
	minAddrLen  uint
	maxAddrLen  uint
	addrLenStep uint
}

func (w transactionWorkchainInfo) validAddrLen(addrLen uint) bool {
	return addrLen >= w.minAddrLen && addrLen <= w.maxAddrLen &&
		(addrLen == w.minAddrLen || addrLen == w.maxAddrLen ||
			(w.addrLenStep > 0 && (addrLen-w.minAddrLen)%w.addrLenStep == 0))
}

func transactionGetWorkchainInfo(cfg tlb.BlockchainConfig, workchain int32) (transactionWorkchainInfo, bool, bool) {
	workchains, err := cfg.GetWorkchains()
	if err != nil || workchains == nil || workchains.Workchains == nil {
		return transactionWorkchainInfo{}, false, false
	}
	value, err := workchains.Workchains.LoadValueByIntKey(big.NewInt(int64(workchain)))
	if err != nil {
		return transactionWorkchainInfo{}, false, true
	}
	info, err := transactionLoadWorkchainInfo(value)
	if err != nil {
		return transactionWorkchainInfo{}, false, true
	}
	return info, true, true
}

func transactionLoadWorkchainInfo(sl *cell.Slice) (transactionWorkchainInfo, error) {
	tag, err := sl.LoadUInt(8)
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	if tag != 0xa6 && tag != 0xa7 {
		return transactionWorkchainInfo{}, fmt.Errorf("unexpected workchain descriptor tag %x", tag)
	}
	if _, err = sl.LoadUInt(32); err != nil {
		return transactionWorkchainInfo{}, err
	}
	if _, err = sl.LoadUInt(8); err != nil {
		return transactionWorkchainInfo{}, err
	}
	monitorMinSplit, err := sl.LoadUInt(8)
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	minSplit, err := sl.LoadUInt(8)
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	if monitorMinSplit > minSplit {
		return transactionWorkchainInfo{}, errors.New("invalid workchain split limits")
	}
	basic, err := sl.LoadBoolBit()
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	if _, err = sl.LoadBoolBit(); err != nil {
		return transactionWorkchainInfo{}, err
	}
	acceptMsgs, err := sl.LoadBoolBit()
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	flags, err := sl.LoadUInt(13)
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	if flags != 0 {
		return transactionWorkchainInfo{}, errors.New("unsupported workchain descriptor flags")
	}
	if _, err = sl.LoadSlice(256); err != nil {
		return transactionWorkchainInfo{}, err
	}
	if _, err = sl.LoadSlice(256); err != nil {
		return transactionWorkchainInfo{}, err
	}
	if _, err = sl.LoadUInt(32); err != nil {
		return transactionWorkchainInfo{}, err
	}
	if basic {
		formatTag, err := sl.LoadUInt(4)
		if err != nil {
			return transactionWorkchainInfo{}, err
		}
		if formatTag != 1 {
			return transactionWorkchainInfo{}, errors.New("invalid basic workchain format")
		}
		if _, err = sl.LoadInt(32); err != nil {
			return transactionWorkchainInfo{}, err
		}
		if _, err = sl.LoadUInt(64); err != nil {
			return transactionWorkchainInfo{}, err
		}
		return transactionWorkchainInfo{
			acceptMsgs:  acceptMsgs,
			minAddrLen:  256,
			maxAddrLen:  256,
			addrLenStep: 256,
		}, nil
	}

	formatTag, err := sl.LoadUInt(4)
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	if formatTag != 0 {
		return transactionWorkchainInfo{}, errors.New("invalid extended workchain format")
	}
	minAddrLen, err := sl.LoadUInt(12)
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	maxAddrLen, err := sl.LoadUInt(12)
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	addrLenStep, err := sl.LoadUInt(12)
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	if minAddrLen < 64 || minAddrLen > maxAddrLen || maxAddrLen > 1023 || addrLenStep > 1023 {
		return transactionWorkchainInfo{}, errors.New("invalid extended workchain address limits")
	}
	workchainTypeID, err := sl.LoadUInt(32)
	if err != nil {
		return transactionWorkchainInfo{}, err
	}
	if workchainTypeID == 0 {
		return transactionWorkchainInfo{}, errors.New("invalid workchain type id")
	}
	return transactionWorkchainInfo{
		acceptMsgs:  acceptMsgs,
		minAddrLen:  uint(minAddrLen),
		maxAddrLen:  uint(maxAddrLen),
		addrLenStep: uint(addrLenStep),
	}, nil
}

type transactionBounceResult struct {
	outMsg          *cell.Cell
	phase           *tlb.BouncePhase
	balance         *big.Int
	extraCurrencies *cell.Dictionary
	msgFees         *big.Int
}

func transactionShouldBounce(msg *tlb.Message, skipReason *tlb.ComputeSkipReason, computeSuccess bool, actionBounce bool) bool {
	if msg == nil || msg.MsgType != tlb.MsgTypeInternal {
		return false
	}
	in := msg.AsInternal()
	if !in.Bounce || in.Bounced {
		return false
	}
	return skipReason != nil || !computeSuccess || actionBounce
}

func transactionPrepareBouncePhase(msg *tlb.Message, balance *big.Int, extraCurrencies *cell.Dictionary, msgBalance *transactionCurrencyBalance, gasFees, actionFine *big.Int, startLT uint64, now uint32, outMsgCount int, cfg tlb.BlockchainConfig, skipReason *tlb.ComputeSkipReason, computeResult *MessageExecutionResult, actionPhase *tlb.ActionPhase) (*transactionBounceResult, error) {
	if msg == nil || msg.MsgType != tlb.MsgTypeInternal {
		return nil, nil
	}
	in := msg.AsInternal()
	if !in.Bounce || in.Bounced {
		return nil, nil
	}

	bounceDstAddr, ok := transactionValidateAndNormalizeInternalDestAddr(in.SrcAddr, cfg)
	if !ok {
		return nil, nil
	}
	bounceBody := transactionBuildBounceBody(in, cfg, skipReason, computeResult, actionPhase)
	extraFlags := transactionInboundExtraFlags(in)
	preliminary := &tlb.InternalMessage{
		IHRDisabled:     true,
		Bounce:          false,
		Bounced:         true,
		SrcAddr:         in.DstAddr,
		DstAddr:         bounceDstAddr,
		Amount:          tlb.FromNanoTONU(0),
		ExtraCurrencies: transactionCloneDictShallow(in.ExtraCurrencies),
		IHRFee:          tlb.FromNanoTON(new(big.Int).SetUint64(extraFlags)),
		FwdFee:          tlb.FromNanoTONU(0),
		CreatedLT:       startLT + 1 + uint64(outMsgCount),
		CreatedAt:       now,
		Body:            bounceBody,
	}
	if _, err := tlb.ToCell(preliminary); err != nil {
		return nil, fmt.Errorf("failed to serialize preliminary bounce message: %w", err)
	}

	msgSize := transactionBounceMessageUsage(in, bounceBody)
	fwdFee := transactionComputeForwardFeeForUsage(cfg, in.DstAddr, bounceDstAddr, msgSize)
	remainingMsgBalance := msgBalance.copy()
	remainingMsgBalance.grams.Sub(remainingMsgBalance.grams, gasFees)
	remainingMsgBalance.grams.Sub(remainingMsgBalance.grams, actionFine)

	out := &transactionBounceResult{
		balance:         transactionBigOrZero(balance),
		extraCurrencies: extraCurrencies,
		msgFees:         big.NewInt(0),
	}
	if remainingMsgBalance.grams.Sign() < 0 || remainingMsgBalance.grams.Cmp(fwdFee) < 0 {
		out.phase = &tlb.BouncePhase{Phase: tlb.BouncePhaseNoFunds{
			MsgSize: tlb.StorageUsedShort{
				Cells: new(big.Int).SetUint64(msgSize.cells),
				Bits:  new(big.Int).SetUint64(msgSize.bits),
			},
			ReqFwdFees: tlb.FromNanoTON(fwdFee),
		}}
		return out, nil
	}

	accountBalance, err := transactionCurrencyFromParts(balance, extraCurrencies)
	if err != nil {
		return nil, err
	}
	if !accountBalance.sub(remainingMsgBalance) {
		accountBalance.grams.SetInt64(0)
		accountBalance.extra = map[uint32]*big.Int{}
	}
	out.balance = accountBalance.grams
	out.extraCurrencies, err = accountBalance.extraDict()
	if err != nil {
		return nil, err
	}
	msgAmount := new(big.Int).Sub(remainingMsgBalance.grams, fwdFee)
	collectedFwdFee := transactionFirstPartForwardFee(cfg, in.DstAddr, bounceDstAddr, fwdFee)
	remainingFwdFee := new(big.Int).Sub(fwdFee, collectedFwdFee)
	preliminary.Amount = tlb.FromNanoTON(msgAmount)
	preliminary.ExtraCurrencies, err = remainingMsgBalance.extraDict()
	if err != nil {
		return nil, err
	}
	preliminary.FwdFee = tlb.FromNanoTON(remainingFwdFee)
	preliminary.IHRFee = tlb.FromNanoTON(new(big.Int).SetUint64(extraFlags))

	bounceCell, err := tlb.ToCell(preliminary)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize bounce message: %w", err)
	}

	out.outMsg = bounceCell
	out.msgFees = collectedFwdFee
	out.phase = &tlb.BouncePhase{Phase: tlb.BouncePhaseOk{
		MsgSize: tlb.StorageUsedShort{
			Cells: new(big.Int).SetUint64(msgSize.cells),
			Bits:  new(big.Int).SetUint64(msgSize.bits),
		},
		MsgFees: tlb.FromNanoTON(collectedFwdFee),
		FwdFees: tlb.FromNanoTON(remainingFwdFee),
	}}
	return out, nil
}

func transactionBuildBounceBody(in *tlb.InternalMessage, cfg tlb.BlockchainConfig, skipReason *tlb.ComputeSkipReason, computeResult *MessageExecutionResult, actionPhase *tlb.ActionPhase) *cell.Cell {
	if in == nil {
		return cell.BeginCell().EndCell()
	}

	flags := transactionInboundExtraFlags(in)
	if flags&1 != 0 {
		body := cell.BeginCell().MustStoreUInt(0xFFFFFFFE, 32)
		originalBody := transactionBounceOriginalBody(in.Body, flags&2 != 0)
		body.MustStoreRef(originalBody)

		valueCell, err := tlb.ToCell(&tlb.CurrencyCollection{
			Coins:           in.Amount,
			ExtraCurrencies: in.ExtraCurrencies,
		})
		if err != nil {
			return cell.BeginCell().EndCell()
		}
		originalInfo := cell.BeginCell().
			MustStoreBuilder(valueCell.ToBuilder()).
			MustStoreUInt(in.CreatedLT, 64).
			MustStoreUInt(uint64(in.CreatedAt), 32).
			EndCell()
		body.MustStoreRef(originalInfo)

		phase, exitCode := transactionBouncePhaseExit(skipReason, computeResult, actionPhase)
		body.MustStoreUInt(uint64(phase), 8)
		body.MustStoreInt(int64(exitCode), 32)
		if skipReason != nil || computeResult == nil {
			body.MustStoreBoolBit(false)
		} else {
			body.MustStoreBoolBit(true)
			body.MustStoreUInt(uint64(computeResult.GasUsed), 32)
			body.MustStoreUInt(uint64(computeResult.Steps), 32)
		}
		return body.EndCell()
	}

	if !transactionHasCapability(cfg, 4) {
		return cell.BeginCell().EndCell()
	}

	body := cell.BeginCell().MustStoreUInt(0xFFFFFFFF, 32)
	if in.Body == nil {
		return body.EndCell()
	}
	bodySlice := in.Body.MustBeginParse()
	bits := bodySlice.BitsLeft()
	if bits > 256 {
		bits = 256
	}
	if bits > 0 {
		body.MustStoreSlice(bodySlice.MustLoadSlice(bits), bits)
	}
	return body.EndCell()
}

func transactionBounceMessageUsage(in *tlb.InternalMessage, body *cell.Cell) transactionUsage {
	usage := transactionUsage{}
	if in == nil {
		return usage
	}
	if body != nil {
		for i := 0; i < int(body.RefsNum()); i++ {
			usage = transactionAddUsage(usage, transactionCollectUsage(body.MustPeekRef(i)))
		}
	}
	return usage
}

func transactionInboundExtraFlags(in *tlb.InternalMessage) uint64 {
	if in == nil || in.IHRFee.Nano() == nil || !in.IHRFee.Nano().IsUint64() {
		return 0
	}
	return in.IHRFee.Nano().Uint64() & 3
}

func transactionBounceOriginalBody(body *cell.Cell, full bool) *cell.Cell {
	if body == nil {
		return cell.BeginCell().EndCell()
	}
	if full {
		return body
	}
	slice := body.MustBeginParse()
	b := cell.BeginCell()
	if bits := slice.BitsLeft(); bits > 0 {
		b.MustStoreSlice(slice.MustLoadSlice(bits), bits)
	}
	return b.EndCell()
}

func transactionBouncePhaseExit(skipReason *tlb.ComputeSkipReason, computeResult *MessageExecutionResult, actionPhase *tlb.ActionPhase) (uint8, int32) {
	if skipReason != nil {
		return 0, -transactionSkipReasonCode(skipReason.Type)
	}
	if computeResult != nil && !vm.IsSuccessExitCode(computeResult.ExitCode) {
		return 1, int32(computeResult.ExitCode)
	}
	if actionPhase != nil {
		return 2, actionPhase.ResultCode
	}
	return 2, 0
}

func transactionSkipReasonCode(reason tlb.ComputeSkipReasonType) int32 {
	switch reason {
	case tlb.ComputeSkipReasonNoState:
		return 1
	case tlb.ComputeSkipReasonBadState:
		return 2
	case tlb.ComputeSkipReasonNoGas:
		return 3
	case tlb.ComputeSkipReasonSuspended:
		return 4
	default:
		return 0
	}
}

func transactionHasCapability(cfg tlb.BlockchainConfig, capability uint64) bool {
	version, err := cfg.GetGlobalVersion()
	return err == nil && version.Capabilities&capability != 0
}

func transactionComputeForwardFeeForMessage(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, msgCell *cell.Cell) *big.Int {
	return transactionComputeForwardFeeForUsage(cfg, srcAddr, dstAddr, transactionMessageTailUsage(msgCell))
}

func transactionComputeForwardFeeForUsage(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, usage transactionUsage) *big.Int {
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil {
		return big.NewInt(0)
	}
	return prices.ComputeForwardFee(usage.cells, usage.bits)
}

func transactionComputeIHRFee(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, fwdFee *big.Int, ihrDisabled bool) *big.Int {
	if ihrDisabled || fwdFee == nil || fwdFee.Sign() == 0 {
		return big.NewInt(0)
	}
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil || prices.IHRFactor == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Rsh(new(big.Int).Mul(fwdFee, new(big.Int).SetUint64(uint64(prices.IHRFactor))), 16)
}

func transactionFirstPartForwardFee(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, fwdFee *big.Int) *big.Int {
	if fwdFee == nil || fwdFee.Sign() == 0 {
		return big.NewInt(0)
	}
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil || prices.FirstFrac == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Rsh(new(big.Int).Mul(fwdFee, new(big.Int).SetUint64(uint64(prices.FirstFrac))), 16)
}

func transactionGetMsgForwardPrices(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address) *tlb.ConfigMsgForwardPrices {
	prices, err := cfg.GetMsgForwardPrices(transactionIsMasterchain(srcAddr) || transactionIsMasterchain(dstAddr))
	if err != nil {
		return nil
	}
	return prices
}

func transactionMessageTailUsage(root *cell.Cell) transactionUsage {
	usage := transactionCollectUsage(root)
	if root == nil || usage.cells == 0 {
		return transactionUsage{}
	}
	usage.cells--
	rootBits := uint64(root.BitsSize())
	if usage.bits >= rootBits {
		usage.bits -= rootBits
	} else {
		usage.bits = 0
	}
	return usage
}

func buildTransactionAccountCell(acc *transactionRuntimeAccount, status tlb.AccountStatus, balance *big.Int, extraCurrencies *cell.Dictionary, endLT uint64, lastPaid uint32, duePayment *tlb.Coins, code, data *cell.Cell, libs *cell.Dictionary, stateHash []byte) (*cell.Cell, *tlb.AccountState, error) {
	if status == tlb.AccountStatusNonExist {
		accountState := &tlb.AccountState{
			IsValid: false,
			AccountStorage: tlb.AccountStorage{
				Status:  tlb.AccountStatusNonExist,
				Balance: tlb.FromNanoTON(big.NewInt(0)),
			},
		}
		accountCell, err := tlb.ToCell(accountState)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to serialize account state: %w", err)
		}
		return accountCell, accountState, nil
	}

	stateInit := &tlb.StateInit{
		TickTock: acc.tickTock,
		Code:     code,
		Data:     data,
		Lib:      libs,
	}

	accountStorage := tlb.AccountStorage{
		Status:            status,
		LastTransactionLT: endLT,
		Balance:           tlb.FromNanoTON(balance),
		ExtraCurrencies:   extraCurrencies,
	}
	switch status {
	case tlb.AccountStatusActive:
		accountStorage.StateInit = stateInit
	case tlb.AccountStatusFrozen:
		if len(stateHash) == 0 {
			stateInitCell, err := tlb.ToCell(stateInit)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to serialize frozen state init: %w", err)
			}
			stateHash = stateInitCell.Hash()
		}
		accountStorage.StateHash = append([]byte(nil), stateHash...)
	case tlb.AccountStatusUninit:
	default:
		return nil, nil, fmt.Errorf("unsupported final account status %s", status)
	}

	storageCell, err := tlb.ToCell(&accountStorage)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize account storage: %w", err)
	}

	usage := transactionAccountStorageUsage(acc, storageCell)

	storageExtra := acc.storageInfo.StorageExtra
	if storageExtra == nil {
		storageExtra = tlb.StorageExtraNone{}
	}

	storageInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: new(big.Int).SetUint64(usage.cells),
			BitsUsed:  new(big.Int).SetUint64(usage.bits),
		},
		StorageExtra: storageExtra,
		LastPaid:     lastPaid,
		DuePayment:   duePayment,
	}

	accountState := &tlb.AccountState{
		IsValid:        true,
		Address:        acc.addr,
		StorageInfo:    storageInfo,
		AccountStorage: accountStorage,
	}

	accountCell, err := tlb.ToCell(accountState)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize account state: %w", err)
	}

	return accountCell, accountState, nil
}

func transactionAccountStorageUsage(acc *transactionRuntimeAccount, storageCell *cell.Cell) transactionUsage {
	if acc != nil && acc.storageCell != nil && transactionAccountStorageRefsUnchanged(acc.storageCell, storageCell) {
		return transactionAccountStorageUsageWithSameRefs(acc.storageInfo.StorageUsed, acc.storageCell, storageCell)
	}
	return transactionCollectUsage(storageCell)
}

func transactionAccountStorageRefsUnchanged(oldStorage, newStorage *cell.Cell) bool {
	if oldStorage == nil || newStorage == nil || oldStorage.RefsNum() != newStorage.RefsNum() {
		return false
	}
	for i := 0; i < int(oldStorage.RefsNum()); i++ {
		if !bytes.Equal(oldStorage.MustPeekRef(i).Hash(), newStorage.MustPeekRef(i).Hash()) {
			return false
		}
	}
	return true
}

func transactionAccountStorageUsageWithSameRefs(old tlb.StorageUsed, oldStorage, newStorage *cell.Cell) transactionUsage {
	cells := transactionStorageUsedUint64(old.CellsUsed)
	bits := transactionStorageUsedUint64(old.BitsUsed)
	if oldStorage == nil || newStorage == nil {
		return transactionUsage{cells: cells, bits: bits}
	}

	oldRootBits := uint64(oldStorage.BitsSize())
	newRootBits := uint64(newStorage.BitsSize())
	switch {
	case newRootBits >= oldRootBits:
		bits += newRootBits - oldRootBits
	case bits >= oldRootBits-newRootBits:
		bits -= oldRootBits - newRootBits
	default:
		bits = 0
	}
	return transactionUsage{cells: cells, bits: bits}
}

func transactionStorageUsedUint64(v *big.Int) uint64 {
	if v == nil || v.Sign() <= 0 {
		return 0
	}
	if !v.IsUint64() {
		return ^uint64(0)
	}
	return v.Uint64()
}

type transactionBuildParams struct {
	accountAddr *address.Address
	startLT     uint64
	prevTxHash  []byte
	prevTxLT    uint64
	now         uint32
	origStatus  tlb.AccountStatus
	endStatus   tlb.AccountStatus
	inMsg       *cell.Cell
	outMsgs     []*cell.Cell
	totalFees   *big.Int
	oldHash     []byte
	newHash     []byte
	description transactionBuildDescriptionParams
}

type transactionBuildDescriptionParams struct {
	msg           *tlb.Message
	creditFirst   bool
	storagePhase  *tlb.StoragePhase
	creditPhase   *tlb.CreditPhase
	computeResult *MessageExecutionResult
	computeGas    vm.Gas
	gasFees       *big.Int
	actionPhase   *tlb.ActionPhase
	bouncePhase   *tlb.BouncePhase
	skipReason    *tlb.ComputeSkipReason
	msgStateUsed  bool
	activated     bool
	destroyed     bool
}

func buildTransactionCell(params transactionBuildParams) (*cell.Cell, error) {
	if params.accountAddr == nil || len(params.accountAddr.Data()) != 32 {
		return nil, errors.New("transaction account address must be standard")
	}

	outDict := cell.NewDict(15)
	for i, msgCell := range params.outMsgs {
		value := cell.BeginCell().MustStoreRef(msgCell).EndCell()
		if err := outDict.SetIntKey(big.NewInt(int64(i)), value); err != nil {
			return nil, fmt.Errorf("failed to store outbound message %d: %w", i, err)
		}
	}

	inMsg := &tlb.Message{}
	if err := tlb.LoadFromCell(inMsg, params.inMsg.MustBeginParse()); err != nil {
		return nil, fmt.Errorf("failed to decode input message: %w", err)
	}

	tx := &tlb.Transaction{
		AccountAddr: append([]byte(nil), params.accountAddr.Data()...),
		LT:          params.startLT,
		PrevTxHash:  append([]byte(nil), params.prevTxHash...),
		PrevTxLT:    params.prevTxLT,
		Now:         params.now,
		OutMsgCount: uint16(len(params.outMsgs)),
		OrigStatus:  params.origStatus,
		EndStatus:   params.endStatus,
		TotalFees:   tlb.CurrencyCollection{Coins: tlb.FromNanoTON(params.totalFees)},
		StateUpdate: tlb.HashUpdate{
			OldHash: append([]byte(nil), params.oldHash...),
			NewHash: append([]byte(nil), params.newHash...),
		},
		Description: buildTransactionDescription(params.description),
	}

	tx.IO.In = inMsg
	tx.IO.Out = &tlb.MessagesList{List: outDict}

	txCell, err := tlb.ToCell(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize transaction: %w", err)
	}

	return txCell, nil
}

func buildTransactionDescription(params transactionBuildDescriptionParams) any {
	if params.skipReason != nil {
		return tlb.TransactionDescriptionOrdinary{
			CreditFirst:  params.creditFirst,
			StoragePhase: params.storagePhase,
			CreditPhase:  params.creditPhase,
			ComputePhase: tlb.ComputePhase{
				Phase: tlb.ComputePhaseSkipped{
					Reason: *params.skipReason,
				},
			},
			Aborted:     true,
			BouncePhase: params.bouncePhase,
			Destroyed:   params.destroyed,
		}
	}

	gasLimit := params.computeGas.Limit
	if params.msg.MsgType == tlb.MsgTypeExternalIn {
		gasLimit = 0
	}

	var gasCredit *big.Int
	if params.computeGas.Credit > 0 {
		gasCredit = big.NewInt(params.computeGas.Credit)
	}

	computeSuccess := params.computeResult != nil && vm.IsSuccessExitCode(params.computeResult.ExitCode)
	actionSuccess := params.actionPhase != nil && params.actionPhase.Success

	return tlb.TransactionDescriptionOrdinary{
		CreditFirst:  params.creditFirst,
		StoragePhase: params.storagePhase,
		CreditPhase:  params.creditPhase,
		ComputePhase: tlb.ComputePhase{
			Phase: tlb.ComputePhaseVM{
				Success:          computeSuccess,
				MsgStateUsed:     false,
				AccountActivated: false,
				GasFees:          tlb.FromNanoTON(params.gasFees),
				Details: tlb.ComputePhaseVMDetails{
					GasUsed:          big.NewInt(params.computeResult.GasUsed),
					GasLimit:         big.NewInt(gasLimit),
					GasCredit:        gasCredit,
					Mode:             0,
					ExitCode:         int32(params.computeResult.ExitCode),
					ExitArg:          transactionComputeExitArg(params.computeResult),
					VMSteps:          params.computeResult.Steps,
					VMInitStateHash:  make([]byte, 32),
					VMFinalStateHash: make([]byte, 32),
				},
			},
		},
		ActionPhase: params.actionPhase,
		Aborted:     !(computeSuccess && actionSuccess),
		BouncePhase: params.bouncePhase,
		Destroyed:   params.destroyed,
	}
}

func transactionComputeExitArg(res *MessageExecutionResult) *int32 {
	if res == nil || vm.IsSuccessExitCode(res.ExitCode) || res.Stack == nil || res.Stack.Len() == 0 {
		return nil
	}

	top, err := res.Stack.Get(0)
	if err != nil {
		return nil
	}

	val, ok := top.(*big.Int)
	if !ok || val == nil || !val.IsInt64() {
		return nil
	}

	exitArg := val.Int64()
	if exitArg == 0 {
		return nil
	}
	if exitArg < math.MinInt32 || exitArg > math.MaxInt32 {
		return nil
	}

	v := int32(exitArg)
	return &v
}

func transactionComputeGasFee(cfg tlb.BlockchainConfig, addr *address.Address, gasUsed uint64) *big.Int {
	prices, err := cfg.GetGasPrices(transactionIsMasterchain(addr))
	if err != nil || prices == nil {
		return big.NewInt(0)
	}

	return prices.ComputeGasPrice(gasUsed)
}

func transactionComputeImportFee(cfg tlb.BlockchainConfig, addr *address.Address, msg *tlb.Message, msgCell *cell.Cell) *big.Int {
	if msg.MsgType != tlb.MsgTypeExternalIn {
		return big.NewInt(0)
	}

	prices, err := cfg.GetMsgForwardPrices(transactionIsMasterchain(addr))
	if err != nil || prices == nil {
		return big.NewInt(0)
	}

	usage := transactionCollectUsage(msgCell)
	if usage.cells == 0 {
		return big.NewInt(0)
	}
	if usage.cells > 0 {
		usage.cells--
	}

	rootBits := uint64(msgCell.BitsSize())
	if usage.bits >= rootBits {
		usage.bits -= rootBits
	} else {
		usage.bits = 0
	}

	return prices.ComputeForwardFee(usage.cells, usage.bits)
}

func transactionComputeStorageFee(cfg tlb.BlockchainConfig, acc *transactionRuntimeAccount, now uint32) (*big.Int, error) {
	if now < acc.storageInfo.LastPaid {
		return nil, fmt.Errorf("transaction unix time %d is before account last_paid %d", now, acc.storageInfo.LastPaid)
	}

	total := big.NewInt(0)
	if acc.storageInfo.DuePayment != nil && acc.storageInfo.DuePayment.Nano().Sign() > 0 {
		total.Add(total, acc.storageInfo.DuePayment.Nano())
	}

	usage := acc.storageInfo.StorageUsed
	if usage.CellsUsed == nil || usage.BitsUsed == nil || now <= acc.storageInfo.LastPaid || acc.storageInfo.LastPaid == 0 {
		return total, nil
	}

	fee, err := cfg.ComputeStorageFee(transactionIsMasterchain(acc.addr), acc.storageInfo.LastPaid, now, usage.BitsUsed.Uint64(), usage.CellsUsed.Uint64())
	if err != nil {
		if errors.Is(err, tlb.ErrBlockchainConfigRootNil) || errors.Is(err, tlb.ErrBlockchainConfigParamAbsent) {
			return total, nil
		}
		return nil, err
	}
	total.Add(total, fee)
	return total, nil
}

func transactionGetStorageDueLimits(cfg tlb.BlockchainConfig, addr *address.Address) transactionStorageDueLimits {
	limits := transactionStorageDueLimits{
		freezeDue: big.NewInt(0),
		deleteDue: big.NewInt(0),
	}
	prices, err := cfg.GetGasPrices(transactionIsMasterchain(addr))
	if err != nil || prices == nil {
		return limits
	}
	limits.freezeDue = new(big.Int).SetUint64(prices.FreezeDueLimit)
	limits.deleteDue = new(big.Int).SetUint64(prices.DeleteDueLimit)
	return limits
}

func transactionInitialComputeStatus(status tlb.AccountStatus) tlb.AccountStatus {
	if status == tlb.AccountStatusNonExist {
		return tlb.AccountStatusUninit
	}
	return status
}

func transactionFinalizeAccountStatus(status tlb.AccountStatus, deleted bool, balance *big.Int, extraCurrencies *cell.Dictionary, activated bool) tlb.AccountStatus {
	if deleted {
		if (balance == nil || balance.Sign() == 0) && transactionExtraDictIsEmpty(extraCurrencies) {
			return tlb.AccountStatusNonExist
		}
		return tlb.AccountStatusUninit
	}
	if status == tlb.AccountStatusUninit && !activated && (balance == nil || balance.Sign() == 0) && transactionExtraDictIsEmpty(extraCurrencies) {
		return tlb.AccountStatusNonExist
	}
	return status
}

func transactionPrepareComputeAccount(acc *transactionRuntimeAccount, status tlb.AccountStatus, deleted bool, msg *tlb.Message) (*transactionRuntimeAccount, bool, *tlb.ComputeSkipReason, error) {
	stateInit := transactionMessageStateInit(msg)
	if deleted {
		return acc, false, &tlb.ComputeSkipReason{Type: transactionNoStateSkipReason(stateInit)}, nil
	}
	if status == tlb.AccountStatusActive {
		if acc.code == nil {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}, nil
		}
		if stateInit != nil && msg.MsgType == tlb.MsgTypeExternalIn {
			stateCell, err := tlb.ToCell(stateInit)
			if err != nil {
				return nil, false, nil, fmt.Errorf("failed to serialize inbound state init: %w", err)
			}
			if !bytes.Equal(stateCell.Hash(), acc.addr.Data()) {
				return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
			}
		}
		return acc, false, nil, nil
	}
	if stateInit == nil {
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}, nil
	}

	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to serialize inbound state init: %w", err)
	}
	switch status {
	case tlb.AccountStatusUninit, tlb.AccountStatusNonExist:
		if acc.addr == nil || !bytes.Equal(stateCell.Hash(), acc.addr.Data()) {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
	case tlb.AccountStatusFrozen:
		if !bytes.Equal(stateCell.Hash(), acc.stateHash) {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
	default:
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}, nil
	}
	if stateInit.Code == nil {
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
	}

	next := *acc
	next.status = tlb.AccountStatusActive
	next.code = stateInit.Code
	next.data = stateInit.Data
	next.libraries = stateInit.Lib
	next.tickTock = stateInit.TickTock
	next.stateHash = nil
	return &next, true, nil, nil
}

func transactionNoStateSkipReason(stateInit *tlb.StateInit) tlb.ComputeSkipReasonType {
	if stateInit != nil {
		return tlb.ComputeSkipReasonBadState
	}
	return tlb.ComputeSkipReasonNoState
}

func transactionMessageStateInit(msg *tlb.Message) *tlb.StateInit {
	if msg == nil {
		return nil
	}
	switch msg.MsgType {
	case tlb.MsgTypeInternal:
		return msg.AsInternal().StateInit
	case tlb.MsgTypeExternalIn:
		return msg.AsExternalIn().StateInit
	default:
		return nil
	}
}

func transactionCoinsNano(coins *tlb.Coins) *big.Int {
	if coins == nil {
		return nil
	}
	return new(big.Int).Set(coins.Nano())
}

func transactionCoinsPtr(nano *big.Int) *tlb.Coins {
	if nano == nil || nano.Sign() == 0 {
		return nil
	}
	coins := tlb.FromNanoTON(nano)
	return &coins
}

func transactionBigOrZero(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
}

func transactionCollectUsage(root *cell.Cell) transactionUsage {
	if root == nil {
		return transactionUsage{}
	}

	seen := map[cell.Hash]struct{}{}

	var walk func(c *cell.Cell) transactionUsage
	walk = func(c *cell.Cell) transactionUsage {
		if c == nil {
			return transactionUsage{}
		}
		key := c.HashKey()
		if _, ok := seen[key]; ok {
			return transactionUsage{}
		}

		seen[key] = struct{}{}
		res := transactionUsage{
			cells: 1,
			bits:  uint64(c.BitsSize()),
		}

		for i := 0; i < int(c.RefsNum()); i++ {
			res = transactionAddUsage(res, walk(c.MustPeekRef(i)))
		}
		return res
	}
	return walk(root)
}

func transactionAddUsage(a, b transactionUsage) transactionUsage {
	return transactionUsage{
		cells: a.cells + b.cells,
		bits:  a.bits + b.bits,
	}
}

func transactionZeroCurrencyBalance() *transactionCurrencyBalance {
	return &transactionCurrencyBalance{
		grams: big.NewInt(0),
		extra: map[uint32]*big.Int{},
	}
}

func transactionCurrencyFromCollection(cc tlb.CurrencyCollection) (*transactionCurrencyBalance, error) {
	return transactionCurrencyFromParts(cc.Coins.Nano(), cc.ExtraCurrencies)
}

func transactionCurrencyFromParts(grams *big.Int, extraDict *cell.Dictionary) (*transactionCurrencyBalance, error) {
	extra, err := transactionLoadExtraCurrencies(extraDict)
	if err != nil {
		return nil, err
	}
	return &transactionCurrencyBalance{
		grams: transactionBigOrZero(grams),
		extra: extra,
	}, nil
}

func (c *transactionCurrencyBalance) copy() *transactionCurrencyBalance {
	if c == nil {
		return transactionZeroCurrencyBalance()
	}
	out := &transactionCurrencyBalance{
		grams: transactionBigOrZero(c.grams),
		extra: map[uint32]*big.Int{},
	}
	for id, amount := range c.extra {
		if amount != nil {
			out.extra[id] = new(big.Int).Set(amount)
		}
	}
	return out
}

func (c *transactionCurrencyBalance) add(other *transactionCurrencyBalance) {
	if c == nil || other == nil {
		return
	}
	c.grams.Add(c.grams, transactionBigOrZero(other.grams))
	if c.extra == nil {
		c.extra = map[uint32]*big.Int{}
	}
	for id, amount := range other.extra {
		if amount == nil || amount.Sign() == 0 {
			continue
		}
		if c.extra[id] == nil {
			c.extra[id] = big.NewInt(0)
		}
		c.extra[id].Add(c.extra[id], amount)
	}
	c.removeZeroExtra()
}

func (c *transactionCurrencyBalance) sub(other *transactionCurrencyBalance) bool {
	if c == nil || other == nil {
		return true
	}
	if c.grams.Cmp(transactionBigOrZero(other.grams)) < 0 {
		return false
	}
	if !c.hasExtra(other.extra) {
		return false
	}
	c.grams.Sub(c.grams, transactionBigOrZero(other.grams))
	for id, amount := range other.extra {
		if amount == nil || amount.Sign() == 0 {
			continue
		}
		c.extra[id].Sub(c.extra[id], amount)
	}
	c.removeZeroExtra()
	return true
}

func (c *transactionCurrencyBalance) clamp(max *transactionCurrencyBalance) {
	if c == nil || max == nil {
		return
	}
	if c.grams.Cmp(max.grams) > 0 {
		c.grams.Set(max.grams)
	}
	for id, amount := range c.extra {
		maxAmount := max.extra[id]
		if maxAmount == nil || maxAmount.Sign() <= 0 {
			delete(c.extra, id)
			continue
		}
		if amount.Cmp(maxAmount) > 0 {
			amount.Set(maxAmount)
		}
	}
	c.removeZeroExtra()
}

func (c *transactionCurrencyBalance) hasExtra(need map[uint32]*big.Int) bool {
	for id, amount := range need {
		if amount == nil || amount.Sign() == 0 {
			continue
		}
		have := c.extra[id]
		if have == nil || have.Cmp(amount) < 0 {
			return false
		}
	}
	return true
}

func (c *transactionCurrencyBalance) extraEmpty() bool {
	return transactionExtraCount(c.extra) == 0
}

func (c *transactionCurrencyBalance) removeZeroExtra() {
	if c == nil {
		return
	}
	for id, amount := range c.extra {
		if amount == nil || amount.Sign() == 0 {
			delete(c.extra, id)
		}
	}
}

func (c *transactionCurrencyBalance) extraDict() (*cell.Dictionary, error) {
	if c == nil {
		return nil, nil
	}
	return transactionStoreExtraCurrencies(c.extra)
}

func transactionLoadExtraCurrencies(dict *cell.Dictionary) (map[uint32]*big.Int, error) {
	out := map[uint32]*big.Int{}
	if dict == nil || dict.IsEmpty() {
		return out, nil
	}
	items, err := dict.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to load extra currencies: %w", err)
	}
	for _, item := range items {
		key, err := item.Key.LoadUInt(32)
		if err != nil {
			return nil, fmt.Errorf("failed to load extra currency id: %w", err)
		}
		amount, err := item.Value.LoadVarUInt(32)
		if err != nil {
			return nil, fmt.Errorf("failed to load extra currency amount: %w", err)
		}
		if item.Value.BitsLeft() != 0 || item.Value.RefsNum() != 0 {
			return nil, errors.New("extra currency amount has trailing data")
		}
		if amount.Sign() > 0 {
			out[uint32(key)] = amount
		}
	}
	return out, nil
}

func transactionStoreExtraCurrencies(extra map[uint32]*big.Int) (*cell.Dictionary, error) {
	if transactionExtraCount(extra) == 0 {
		return nil, nil
	}
	dict := cell.NewDict(32)
	for id, amount := range extra {
		if amount == nil || amount.Sign() == 0 {
			continue
		}
		if amount.Sign() < 0 {
			return nil, errors.New("negative extra currency amount")
		}
		value := cell.BeginCell().MustStoreBigVarUInt(amount, 32).EndCell()
		if err := dict.SetIntKey(new(big.Int).SetUint64(uint64(id)), value); err != nil {
			return nil, fmt.Errorf("failed to store extra currency: %w", err)
		}
	}
	return dict, nil
}

func transactionExtraCount(extra map[uint32]*big.Int) uint64 {
	var count uint64
	for _, amount := range extra {
		if amount != nil && amount.Sign() > 0 {
			count++
		}
	}
	return count
}

func transactionCloneExtraCurrencies(dict *cell.Dictionary) (*cell.Dictionary, error) {
	extra, err := transactionLoadExtraCurrencies(dict)
	if err != nil {
		return nil, err
	}
	return transactionStoreExtraCurrencies(extra)
}

func transactionAddExtraCurrencies(a, b *cell.Dictionary) (*cell.Dictionary, error) {
	left, err := transactionCurrencyFromParts(big.NewInt(0), a)
	if err != nil {
		return nil, err
	}
	right, err := transactionCurrencyFromParts(big.NewInt(0), b)
	if err != nil {
		return nil, err
	}
	left.add(right)
	return left.extraDict()
}

func transactionExtraDictIsEmpty(dict *cell.Dictionary) bool {
	if dict == nil || dict.IsEmpty() {
		return true
	}
	extra, err := transactionLoadExtraCurrencies(dict)
	return err != nil || transactionExtraCount(extra) == 0
}

func transactionCloneDictShallow(dict *cell.Dictionary) *cell.Dictionary {
	if dict == nil || dict.IsEmpty() {
		return nil
	}
	return dict.Copy()
}

func transactionMinBig(a, b *big.Int) *big.Int {
	if a == nil {
		return big.NewInt(0)
	}
	if b == nil || a.Cmp(b) <= 0 {
		return new(big.Int).Set(a)
	}
	return new(big.Int).Set(b)
}

func transactionNormalizeBits256(src []byte) []byte {
	if len(src) == 32 {
		return src
	}
	out := make([]byte, 32)
	if len(src) > 32 {
		copy(out, src[len(src)-32:])
	} else {
		copy(out[32-len(src):], src)
	}
	return out
}

func transactionGetSizeLimits(cfg tlb.BlockchainConfig) transactionSizeLimits {
	out := transactionSizeLimits{
		maxMsgBits:            1 << 21,
		maxMsgCells:           1 << 13,
		maxLibraryCells:       1000,
		maxAccStateCells:      1 << 16,
		maxMCAccStateCells:    1 << 11,
		maxAccPublicLibraries: 256,
		maxMsgExtraCurrencies: 2,
	}
	limits, err := cfg.GetSizeLimitsConfig()
	if err != nil {
		return out
	}
	switch v := limits.Config.(type) {
	case tlb.SizeLimitsConfigV1:
		out.maxMsgBits = uint64(v.MaxMsgBits)
		out.maxMsgCells = uint64(v.MaxMsgCells)
		out.maxLibraryCells = uint64(v.MaxLibraryCells)
	case tlb.SizeLimitsConfigV2:
		out.maxMsgBits = uint64(v.MaxMsgBits)
		out.maxMsgCells = uint64(v.MaxMsgCells)
		out.maxLibraryCells = uint64(v.MaxLibraryCells)
		out.maxAccStateCells = uint64(v.MaxAccStateCells)
		out.maxMCAccStateCells = uint64(v.MaxMCAccStateCells)
		out.maxAccPublicLibraries = uint64(v.MaxAccPublicLibraries)
		out.maxMsgExtraCurrencies = uint64(v.MaxMsgExtraCurrencies)
	}
	return out
}

func transactionCheckOutboundMessageSize(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, msgCell *cell.Cell, available *big.Int) (int32, *big.Int) {
	limits := transactionGetSizeLimits(cfg)
	usage := transactionMessageTailUsage(msgCell)
	if usage.bits <= limits.maxMsgBits && usage.cells <= limits.maxMsgCells && transactionMaxMerkleDepth(msgCell) <= 2 {
		return 0, big.NewInt(0)
	}
	return 40, transactionComputeActionFine(cfg, srcAddr, dstAddr, msgCell, available)
}

func transactionComputeActionFine(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, msgCell *cell.Cell, available *big.Int) *big.Int {
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil {
		return big.NewInt(0)
	}
	finePerCell := (prices.CellPrice >> 16) / 4
	if finePerCell == 0 {
		return big.NewInt(0)
	}
	usage := transactionMessageTailUsage(msgCell)
	fine := new(big.Int).Mul(new(big.Int).SetUint64(finePerCell), new(big.Int).SetUint64(usage.cells))
	if available != nil && fine.Cmp(available) > 0 {
		fine.Set(available)
	}
	return fine
}

func transactionAccountStateExceedsLimits(acc *transactionRuntimeAccount, code, data *cell.Cell, libs *cell.Dictionary, cfg tlb.BlockchainConfig) (bool, error) {
	if transactionCellEqual(acc.code, code) && transactionCellEqual(acc.data, data) && transactionDictEqual(acc.libraries, libs) {
		return false, nil
	}
	limits := transactionGetSizeLimits(cfg)
	maxCells := limits.maxAccStateCells
	if transactionIsMasterchain(acc.addr) {
		maxCells = limits.maxMCAccStateCells
	}
	usage := transactionCollectUniqueUsage(code, data)
	if libs != nil && !libs.IsEmpty() {
		usage = transactionAddUsage(usage, transactionCollectUsage(libs.AsCell()))
	}
	if usage.cells > maxCells {
		return true, nil
	}
	if transactionIsMasterchain(acc.addr) && transactionPublicLibrariesCount(libs) > limits.maxAccPublicLibraries {
		return true, nil
	}
	return false, nil
}

func transactionCellEqual(a, b *cell.Cell) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return bytes.Equal(a.Hash(), b.Hash())
}

func transactionDictEqual(a, b *cell.Dictionary) bool {
	if a == nil || a.IsEmpty() {
		return b == nil || b.IsEmpty()
	}
	if b == nil || b.IsEmpty() {
		return false
	}
	return bytes.Equal(a.AsCell().Hash(), b.AsCell().Hash())
}

func transactionCollectUniqueUsage(roots ...*cell.Cell) transactionUsage {
	seen := map[cell.Hash]struct{}{}
	var walk func(c *cell.Cell) transactionUsage
	walk = func(c *cell.Cell) transactionUsage {
		if c == nil {
			return transactionUsage{}
		}
		key := c.HashKey()
		if _, ok := seen[key]; ok {
			return transactionUsage{}
		}
		seen[key] = struct{}{}
		res := transactionUsage{
			cells: 1,
			bits:  uint64(c.BitsSize()),
		}
		for i := 0; i < int(c.RefsNum()); i++ {
			res = transactionAddUsage(res, walk(c.MustPeekRef(i)))
		}
		return res
	}
	out := transactionUsage{}
	for _, root := range roots {
		out = transactionAddUsage(out, walk(root))
	}
	return out
}

func transactionPublicLibrariesCount(libs *cell.Dictionary) uint64 {
	if libs == nil || libs.IsEmpty() {
		return 0
	}
	items, err := libs.LoadAll(true)
	if err != nil {
		return 0
	}
	var count uint64
	for _, item := range items {
		isPublic, err := item.Value.LoadBoolBit()
		if err == nil && isPublic {
			count++
		}
	}
	return count
}

func transactionMaxMerkleDepth(root *cell.Cell) uint16 {
	var maxDepth uint16
	var walk func(c *cell.Cell, depth uint16)
	walk = func(c *cell.Cell, depth uint16) {
		if c == nil {
			return
		}
		switch c.GetType() {
		case cell.MerkleProofCellType, cell.MerkleUpdateCellType:
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		}
		for i := 0; i < int(c.RefsNum()); i++ {
			walk(c.MustPeekRef(i), depth)
		}
	}
	walk(root, 0)
	return maxDepth
}

func transactionIsMasterchain(addr *address.Address) bool {
	return addr != nil && addr.Type() == address.StdAddress && addr.Workchain() == -1
}
