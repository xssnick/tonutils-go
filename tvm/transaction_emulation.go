package tvm

import (
	"errors"
	"fmt"
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
	Accepted           bool
	Transaction        *tlb.Transaction
	TransactionCell    *cell.Cell
	AccountState       *tlb.AccountState
	AccountCell        *cell.Cell
	ShardAccount       *tlb.ShardAccount
	ShardAccountCell   *cell.Cell
	AccountStorageStat *cell.Cell
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
	stateDepth      *uint64
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
	maxMsgBits                  uint64
	maxMsgCells                 uint64
	maxLibraryCells             uint64
	maxAccStateCells            uint64
	maxMCAccStateCells          uint64
	maxAccPublicLibraries       uint64
	maxMsgExtraCurrencies       uint64
	accStateCellsForStorageDict uint64
}

func (tvm *TVM) EmulateTransaction(shard *tlb.ShardAccount, msgCell *cell.Cell, cfg TransactionEmulationConfig) (*TransactionExecutionResult, error) {
	if shard == nil {
		return nil, errors.New("shard account is required")
	}
	if msgCell == nil {
		return nil, errors.New("input message is required")
	}

	var msg tlb.Message
	if err := tlb.Parse(&msg, msgCell); err != nil {
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
	isSpecial := transactionIsSpecialAccount(blockchainCfg, runtimeAcc.addr)

	storageDueLimits := transactionGetStorageDueLimits(blockchainCfg, runtimeAcc.addr)

	storageFee := big.NewInt(0)
	if !isSpecial {
		storageFee, err = transactionComputeStorageFee(blockchainCfg, runtimeAcc, now)
		if err != nil {
			return nil, err
		}
	}
	importFee := big.NewInt(0)
	if !isSpecial {
		importFee = transactionComputeImportFee(blockchainCfg, runtimeAcc.addr, &msg, msgCell)
	}
	prepared, err := transactionPrepareInitialPhases(runtimeAcc, &msg, storageFee, importFee, now, storageDueLimits)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		prepared.lastPaid = 0
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
		gas = transactionMessageGas(execCfg, blockchainCfg, runtimeAcc.addr, prepared.balance, prepared.msgBalance.grams, msg.MsgType, isSpecial)
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
		if precompiledGas, ok, err := transactionPrecompiledGasUsage(execCfg.PrecompiledGasUsage); err != nil {
			return nil, err
		} else if ok {
			msgRes.GasUsed = precompiledGas
			msgRes.Steps = 0
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
	if msgRes != nil && msgRes.Accepted && !isSpecial {
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

	computeSuccess := transactionComputeSucceeded(msgRes)
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
	nextAccountCell, nextAccountState, nextAccountStorageStat, err := buildTransactionAccountCell(computeAcc, finalStatus, finalBalance, nextExtraCurrencies, endLT, prepared.lastPaid, prepared.duePayment, nextCode, nextData, nextLibraries, nextStateHash, blockchainCfg, cfg.AccountStorageStat)
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

	return out, fillTransactionExecutionResult(out, txCell, nextAccountCell, nextAccountState, nextAccountStorageStat, startLT)
}

func (tvm *TVM) EmulateTickTockTransaction(shard *tlb.ShardAccount, isTock bool, cfg TransactionEmulationConfig) (*TransactionExecutionResult, error) {
	if shard == nil {
		return nil, errors.New("shard account is required")
	}

	var proof *cell.MerkleProofBuilder
	if cfg.BuildProof {
		if shard.Account == nil {
			return nil, errors.New("shard account root is nil")
		}
		proof = cell.NewMerkleProofBuilder(shard.Account)
	}

	runtimeAcc, err := loadTransactionRuntimeAccount(shard, nil, proof)
	if err != nil {
		return nil, err
	}
	if runtimeAcc.status != tlb.AccountStatusActive {
		return nil, errors.New("tick/tock transaction requires active account")
	}
	if runtimeAcc.tickTock == nil {
		return nil, errors.New("account has no tick/tock special flag")
	}
	if isTock && !runtimeAcc.tickTock.Tock {
		return nil, errors.New("account does not allow tock transactions")
	}
	if !isTock && !runtimeAcc.tickTock.Tick {
		return nil, errors.New("account does not allow tick transactions")
	}

	now := cfg.Now
	if now == 0 {
		now = uint32(time.Now().Unix())
	}

	blockchainCfg := tlb.BlockchainConfig{
		Root: cfg.ConfigRoot,
	}
	storageDueLimits := transactionGetStorageDueLimits(blockchainCfg, runtimeAcc.addr)
	storageFee := big.NewInt(0)

	extraCurrencies, err := transactionCloneExtraCurrencies(runtimeAcc.extraCurrencies)
	if err != nil {
		return nil, err
	}
	prepared := &transactionPreparedPhases{
		balance:         new(big.Int).Set(runtimeAcc.balance),
		extraCurrencies: extraCurrencies,
		msgBalance:      transactionZeroCurrencyBalance(),
		status:          runtimeAcc.status,
		duePayment:      transactionCoinsPtr(transactionCoinsNano(runtimeAcc.storageInfo.DuePayment)),
		lastPaid:        runtimeAcc.storageInfo.LastPaid,
	}
	prepared.applyStoragePhase(runtimeAcc, storageFee, now, storageDueLimits, false)
	prepared.lastPaid = 0

	execCfg := cfg
	execCfg.Address = runtimeAcc.addr
	execCfg.Now = now
	if execCfg.BlockLT == 0 {
		execCfg.BlockLT = execCfg.LogicalTime
	}
	execCfg.Balance = new(big.Int).Set(prepared.balance)

	var skipReason *tlb.ComputeSkipReason
	gas := transactionTickTockGas(execCfg, blockchainCfg, runtimeAcc.addr)
	if prepared.balance.Sign() <= 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	} else if prepared.status != tlb.AccountStatusActive || prepared.deleted || runtimeAcc.code == nil {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}
	} else if gas.Limit == 0 && gas.Credit == 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	}

	var msgRes *MessageExecutionResult
	if skipReason == nil {
		msgRes, err = tvm.executeTickTockTransaction(runtimeAcc, isTock, execCfg, gas, proof)
		if err != nil {
			return nil, err
		}
		if precompiledGas, ok, err := transactionPrecompiledGasUsage(execCfg.PrecompiledGasUsage); err != nil {
			return nil, err
		} else if ok {
			msgRes.GasUsed = precompiledGas
			msgRes.Steps = 0
		}
	}

	out := &TransactionExecutionResult{}
	if msgRes != nil {
		out.ExecutionResult = msgRes.ExecutionResult
		out.Accepted = msgRes.Accepted
	}

	startLT := transactionStartLT(runtimeAcc.storageLT, execCfg.LogicalTime, nil)
	endLT := startLT + 1
	nextCode := runtimeAcc.code
	nextData := runtimeAcc.data
	nextLibraries := runtimeAcc.libraries
	nextExtraCurrencies := prepared.extraCurrencies
	var outMessages []*cell.Cell
	var actionPhase *tlb.ActionPhase
	var actionDeleted bool
	gasFees := big.NewInt(0)
	finalBalance := new(big.Int).Sub(prepared.balance, gasFees)
	if finalBalance.Sign() < 0 {
		return nil, errors.New("transaction fees exceed account balance")
	}

	actionFees := big.NewInt(0)
	if transactionComputeSucceeded(msgRes) {
		actionRes, applyErr := transactionApplyActions(runtimeAcc, msgRes, startLT, now, blockchainCfg, finalBalance, nextExtraCurrencies, prepared.msgBalance, gasFees)
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
		actionDeleted = actionRes.deleteAccount
	}

	totalFees := new(big.Int).Set(prepared.storagePhase.StorageFeesCollected.Nano())
	totalFees.Add(totalFees, gasFees)
	totalFees.Add(totalFees, actionFees)

	computeSuccess := transactionComputeSucceeded(msgRes)
	actionSuccess := actionPhase != nil && actionPhase.Success
	if computeSuccess && actionSuccess {
		nextData = msgRes.Data
	}
	if nextCode == nil {
		nextCode = runtimeAcc.code
	}
	if nextLibraries == nil {
		nextLibraries = runtimeAcc.libraries
	}

	if actionDeleted {
		prepared.deleted = true
	}
	finalStatus := transactionFinalizeAccountStatus(prepared.status, prepared.deleted, finalBalance, nextExtraCurrencies, false)
	nextStateHash := runtimeAcc.stateHash
	if finalStatus == tlb.AccountStatusFrozen && runtimeAcc.status == tlb.AccountStatusActive {
		nextStateHash = nil
	}
	nextAccountCell, nextAccountState, nextAccountStorageStat, err := buildTransactionAccountCell(runtimeAcc, finalStatus, finalBalance, nextExtraCurrencies, endLT, prepared.lastPaid, prepared.duePayment, nextCode, nextData, nextLibraries, nextStateHash, blockchainCfg, cfg.AccountStorageStat)
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
		outMsgs:     outMessages,
		totalFees:   totalFees,
		oldHash:     runtimeAcc.originalCell.Hash(),
		newHash:     nextAccountCell.Hash(),
		description: transactionBuildDescriptionParams{
			tickTock:      true,
			isTock:        isTock,
			storagePhase:  prepared.storagePhase,
			computeResult: msgRes,
			computeGas:    gas,
			gasFees:       gasFees,
			actionPhase:   actionPhase,
			skipReason:    skipReason,
			destroyed:     prepared.deleted,
		},
	})
	if err != nil {
		return nil, err
	}

	return out, fillTransactionExecutionResult(out, txCell, nextAccountCell, nextAccountState, nextAccountStorageStat, startLT)
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
		bodySlice, err := body.BeginParse()
		if err != nil {
			return nil, err
		}
		if err := stack.PushSlice(bodySlice); err != nil {
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
		bodySlice, err := body.BeginParse()
		if err != nil {
			return nil, err
		}
		if err := stack.PushSlice(bodySlice); err != nil {
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

func (tvm *TVM) executeTickTockTransaction(acc *transactionRuntimeAccount, isTock bool, cfg TransactionEmulationConfig, gas vm.Gas, proof *cell.MerkleProofBuilder) (*MessageExecutionResult, error) {
	accAddr, err := messageEmulationAccountAddr(acc.addr)
	if err != nil {
		return nil, err
	}

	stack := vm.NewStack()
	balance := new(big.Int).Set(cfg.Balance)
	if err = stack.PushInt(balance); err != nil {
		return nil, err
	}
	if err = stack.PushInt(accAddr); err != nil {
		return nil, err
	}
	if err = stack.PushBool(isTock); err != nil {
		return nil, err
	}
	if err = stack.PushInt(big.NewInt(-2)); err != nil {
		return nil, err
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
