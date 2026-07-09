package tvm

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// TransactionOptions carries the genuinely per-transaction execution inputs.
// Everything else lives in BlockContext (per block) and PreparedBlockchainConfig (per
// config epoch).
type TransactionOptions struct {
	// LogicalTime is the minimal logical time of the transaction. The
	// effective start LT also respects the account's last transaction LT and
	// the inbound message LT. Zero derives it from the account state.
	LogicalTime int64
	// RandSeed is the per-account random seed (c7[6]) as raw bytes. When
	// empty, it is derived from the block rand seed and the account address
	// (see BlockContext.AccountRandSeed).
	RandSeed []byte
	// Gas overrides the config-derived gas limits when non-zero.
	Gas vm.Gas
	// AccountStorageStat is the account storage-stat dictionary carried in
	// from the previous transaction result of the same account.
	AccountStorageStat *cell.Cell
	// BuildProof builds a Merkle usage proof over the account state covering
	// the compute-phase reads (returned in ExecutionResult.Proof).
	BuildProof bool
	// SignatureCheckAlwaysSucceed makes signature checks succeed unconditionally
	// (fee estimation flows).
	SignatureCheckAlwaysSucceed bool
	// TraceHook observes VM execution.
	TraceHook vm.TraceHook
}

// OutMessage is an outbound message emitted by a transaction, in creation
// order. Msg is the parsed form of Cell built during the action phase.
type OutMessage struct {
	Cell *cell.Cell
	Msg  *tlb.Message
}

type TransactionExecutionResult struct {
	ExecutionResult
	Accepted        bool
	TransactionCell *cell.Cell
	// NextAccount is the resulting account state, prepared to feed the next
	// transaction of the same account without any re-parsing.
	NextAccount *PreparedAccount
	// OutMessages are the emitted outbound messages in creation order,
	// including the bounce message when one was produced.
	OutMessages []OutMessage
	// EndLT is the logical time after the transaction (start LT + 1 + number
	// of emitted messages).
	EndLT uint64
	// AccountStorageStat is the account storage-stat dictionary to pass to the
	// next transaction of the same account.
	AccountStorageStat *cell.Cell
}

// ParseTransaction parses the built transaction cell into its tlb form. The
// executor itself never needs the parsed transaction, so the parse is done on
// demand only.
func (r *TransactionExecutionResult) ParseTransaction() (*tlb.Transaction, error) {
	if r == nil || r.TransactionCell == nil {
		return nil, errors.New("transaction result has no transaction cell")
	}
	var tx tlb.Transaction
	if err := tlb.Parse(&tx, r.TransactionCell); err != nil {
		return nil, fmt.Errorf("failed to decode built transaction: %w", err)
	}
	tx.Hash = r.TransactionCell.Hash()
	return &tx, nil
}

// PreparedMessage is an inbound message parsed once and reused by the
// executor without re-parsing.
type PreparedMessage struct {
	cell *cell.Cell
	msg  tlb.Message
}

// PrepareMessage parses an inbound message cell once and validates that it can
// be used as a transaction input.
func PrepareMessage(msgCell *cell.Cell) (*PreparedMessage, error) {
	if msgCell == nil {
		return nil, errors.New("input message is required")
	}
	var msg tlb.Message
	if err := tlb.Parse(&msg, msgCell); err != nil {
		return nil, fmt.Errorf("failed to decode input message: %w", err)
	}
	return prepareParsedMessage(msgCell, msg)
}

// PrepareParsedMessage wraps an already-parsed inbound message together with
// its cell; msg must be the parsed form of msgCell.
func PrepareParsedMessage(msgCell *cell.Cell, msg *tlb.Message) (*PreparedMessage, error) {
	if msgCell == nil {
		return nil, errors.New("input message is required")
	}
	if msg == nil {
		return nil, errors.New("parsed input message is required")
	}
	return prepareParsedMessage(msgCell, *msg)
}

func prepareParsedMessage(msgCell *cell.Cell, msg tlb.Message) (*PreparedMessage, error) {
	switch msg.MsgType {
	case tlb.MsgTypeInternal, tlb.MsgTypeExternalIn:
	case tlb.MsgTypeExternalOut:
		return nil, errors.New("external outbound messages cannot be used as transaction input")
	default:
		return nil, fmt.Errorf("unsupported input message type %s", msg.MsgType)
	}
	if err := transactionValidateMessageStateInitLibs(&msg); err != nil {
		return nil, err
	}
	return &PreparedMessage{cell: msgCell, msg: msg}, nil
}

// Cell returns the raw message cell.
func (m *PreparedMessage) Cell() *cell.Cell {
	return m.cell
}

// Message returns the parsed message.
func (m *PreparedMessage) Message() *tlb.Message {
	return &m.msg
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
	inMsgLibraries  *cell.Dictionary
	stateDepth      *uint64
	tickTock        *tlb.TickTock
	stateHash       []byte
	storageLT       uint64
	storageCell     *cell.Cell
	prevTxHash      []byte
	prevTxLT        uint64
	originalCell    *cell.Cell
	isSpecial       bool
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
	destroyed       bool
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
	maxExtMsgDepth              uint16
	maxAccStateCells            uint64
	maxMCAccStateCells          uint64
	maxAccPublicLibraries       uint64
	maxMsgExtraCurrencies       uint64
	maxAccFixedPrefixLength     uint64
	accStateCellsForStorageDict uint64
}

// transactionExecEnv is the assembled per-transaction execution environment:
// block context plus derived phase values feeding the c7 tuple.
type transactionExecEnv struct {
	block *BlockContext
	cfg   *PreparedBlockchainConfig
	opts  *TransactionOptions

	acc     *transactionRuntimeAccount
	msg     *tlb.Message
	msgCell *cell.Cell

	startLT uint64
	blockLT int64
	balance *big.Int

	incomingValue       tuple.Tuple
	storageFees         int64
	duePayment          *big.Int
	inMsgParams         tuple.Tuple
	precompiledGasUsage *big.Int

	proof        *cell.MerkleProofBuilder
	stopOnAccept bool
}

func newTransactionExecEnv(block *BlockContext, opts *TransactionOptions, acc *transactionRuntimeAccount, msg *tlb.Message, msgCell *cell.Cell, prepared *transactionPreparedPhases, startLT uint64) *transactionExecEnv {
	env := &transactionExecEnv{
		block:   block,
		cfg:     block.cfg,
		opts:    opts,
		acc:     acc,
		msg:     msg,
		msgCell: msgCell,
		startLT: startLT,
		blockLT: block.blockLT,
		balance: new(big.Int).Set(prepared.balance),
	}
	if env.blockLT == 0 {
		env.blockLT = transactionBlockLogicalTime(startLT)
	}
	env.incomingValue = prepared.msgBalance.asTuple()
	env.storageFees = transactionInt64OrZero(prepared.storagePhase.StorageFeesCollected.Nano())
	env.duePayment = transactionBigOrZero(transactionCoinsNano(prepared.duePayment))
	env.inMsgParams = transactionBuildInMsgParams(msg, prepared.msgBalance)
	return env
}

func (env *transactionExecEnv) buildC7(code *cell.Cell, balance *big.Int) (tuple.Tuple, error) {
	seed, err := transactionSeed(env.block, env.opts, env.acc.addr)
	if err != nil {
		return tuple.Tuple{}, err
	}
	return buildEmulationC7(emulationC7Input{
		addr:                env.acc.addr,
		code:                code,
		now:                 env.block.now,
		blockLT:             env.blockLT,
		logicalTime:         int64(env.startLT),
		balance:             balance,
		seed:                seed,
		configRoot:          env.cfg.root,
		incomingValue:       env.incomingValue,
		storageFees:         env.storageFees,
		prevBlocks:          env.block.prevBlocksValue(),
		unpackedConfig:      env.block.unpackedConfig,
		duePayment:          transactionMaybeBigValue(env.duePayment),
		precompiledGasUsage: env.precompiledGasUsage,
		inMsgParams:         env.inMsgParams,
		globalVersion:       env.cfg.version,
	})
}

// transactionSeed resolves the c7 rand seed: the explicit per-account seed
// when provided, otherwise derived from the block seed and account address.
func transactionSeed(block *BlockContext, opts *TransactionOptions, addr *address.Address) (*big.Int, error) {
	if len(opts.RandSeed) > 0 {
		return new(big.Int).SetBytes(opts.RandSeed), nil
	}
	seed, err := accountRandSeedBytes(block.randSeed, addr, block.cfg.version)
	if err != nil {
		return nil, err
	}
	if len(seed) == 0 {
		return big.NewInt(0), nil
	}
	return new(big.Int).SetBytes(seed), nil
}

func transactionMaybeBigValue(v *big.Int) any {
	if v == nil {
		return nil
	}
	return v
}

// transactionExecutionLibraries combines the compute-phase library collections
// the reference implementation uses. Since global version 15, account-private
// and inbound StateInit libraries are excluded from the VM context.
func transactionExecutionLibraries(acc *transactionRuntimeAccount, blockLibraries []*cell.Cell, globalVersion uint32) []*cell.Cell {
	if globalVersion >= 15 {
		return blockLibraries
	}

	var inMsgLibrary, accountLibrary *cell.Cell
	if acc.inMsgLibraries != nil {
		inMsgLibrary = acc.inMsgLibraries.AsCell()
	}
	if acc.libraries != nil {
		accountLibrary = acc.libraries.AsCell()
	}
	if inMsgLibrary == nil && accountLibrary == nil {
		return blockLibraries
	}

	librariesNum := len(blockLibraries)
	if inMsgLibrary != nil {
		librariesNum++
	}
	if accountLibrary != nil {
		librariesNum++
	}

	libraries := make([]*cell.Cell, librariesNum)
	idx := 0
	if inMsgLibrary != nil {
		libraries[idx] = inMsgLibrary
		idx++
	}
	if accountLibrary != nil {
		libraries[idx] = accountLibrary
		idx++
	}
	copy(libraries[idx:], blockLibraries)
	return libraries
}

// EmulateTransaction executes an ordinary transaction of acc with the inbound
// message msg. The result feeds the next transaction of the same account
// through NextAccount and AccountStorageStat.
func (tvm *TVM) EmulateTransaction(block *BlockContext, acc *PreparedAccount, msg *PreparedMessage, opts TransactionOptions) (*TransactionExecutionResult, error) {
	if block == nil {
		return nil, errors.New("block context is required")
	}
	if acc == nil {
		return nil, errors.New("prepared account is required")
	}
	if msg == nil {
		return nil, errors.New("prepared message is required")
	}
	blockchainCfg := block.cfg

	runtimeAcc, proof, err := acc.runtimeForExecution(opts.BuildProof)
	if err != nil {
		return nil, err
	}

	now := block.now
	if err = transactionValidateInboundExternalMessage(msg.cell, &msg.msg, blockchainCfg); err != nil {
		return nil, err
	}
	isSpecial := blockchainCfg.isSpecialAccount(runtimeAcc.addr)
	runtimeAcc.isSpecial = isSpecial

	storageDueLimits := blockchainCfg.storageDueLimitsFor(transactionIsMasterchain(runtimeAcc.addr))

	storageFee, err := transactionComputeStorageFee(blockchainCfg, runtimeAcc, now)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		storageFee = transactionCoinsNano(runtimeAcc.storageInfo.DuePayment)
	}
	importFee := big.NewInt(0)
	if !isSpecial {
		importFee, err = transactionComputeImportFee(blockchainCfg, runtimeAcc.addr, &msg.msg, msg.cell)
		if err != nil {
			return nil, err
		}
	}
	prepared, err := transactionPrepareInitialPhases(runtimeAcc, &msg.msg, storageFee, importFee, now, blockchainCfg, storageDueLimits)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		prepared.lastPaid = 0
	}

	startLT := transactionStartLT(runtimeAcc.storageLT, transactionExecutionLogicalTime(runtimeAcc.prevTxLT, opts.LogicalTime), &msg.msg)
	env := newTransactionExecEnv(block, &opts, runtimeAcc, &msg.msg, msg.cell, prepared, startLT)
	env.proof = proof

	computeAcc := runtimeAcc
	msgStateUsed := false
	var skipReason *tlb.ComputeSkipReason
	var gas vm.Gas
	if prepared.balance.Sign() <= 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	} else {
		gas = transactionMessageGas(opts.Gas, now, blockchainCfg, runtimeAcc.addr, prepared.balance, prepared.msgBalance.grams, msg.msg.MsgType, isSpecial)
		if gas.Limit == 0 && gas.Credit == 0 {
			skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
		} else {
			addressSuspended := false
			if !prepared.deleted && (prepared.status == tlb.AccountStatusUninit || prepared.status == tlb.AccountStatusNonExist) && transactionMessageStateInit(&msg.msg) != nil {
				addressSuspended = blockchainCfg.isAddressSuspended(now, runtimeAcc.addr)
			}

			computeAcc, msgStateUsed, skipReason, err = transactionPrepareComputeAccount(runtimeAcc, prepared.status, prepared.deleted, &msg.msg, addressSuspended, blockchainCfg)
			if err != nil {
				return nil, err
			}
			if opts.BuildProof && msgStateUsed && skipReason == nil {
				return nil, errors.New("account execution proof cannot be built for code loaded from message state init")
			}
			if skipReason == nil {
				gas, skipReason = transactionApplyPrecompiledGasConfig(blockchainCfg, computeAcc.code, gas, env)
			}
		}
	}

	var msgRes *MessageExecutionResult
	accountActivated := false
	if skipReason == nil {
		msgRes, err = tvm.executeTransactionMessage(computeAcc, env, gas, prepared.msgBalance.grams)
		if err != nil {
			return nil, err
		}
		transactionNormalizeGasUsage(msgRes)
		if err = transactionApplyPrecompiledGasUsage(msgRes, env.precompiledGasUsage); err != nil {
			return nil, err
		}
		if msgStateUsed && msgRes.Accepted {
			accountActivated = true
			prepared.status = tlb.AccountStatusActive
			prepared.deleted = false
			prepared.destroyed = false
		}
	}

	out := &TransactionExecutionResult{}
	if msgRes != nil {
		out.ExecutionResult = msgRes.ExecutionResult
		out.Accepted = msgRes.Accepted
	}
	if msg.msg.MsgType == tlb.MsgTypeExternalIn && (msgRes == nil || !msgRes.Accepted) {
		return out, nil
	}

	endLT := startLT + 1
	nextCode := computeAcc.code
	nextData := computeAcc.data
	nextLibraries := computeAcc.libraries
	nextExtraCurrencies := prepared.extraCurrencies
	msgBalanceRemaining := prepared.msgBalance
	var outMessages []OutMessage
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
		msgBalanceRemaining = actionRes.msgBalanceRemaining
	}

	totalFees := new(big.Int).Set(prepared.storagePhase.StorageFeesCollected.Nano())
	totalFees.Add(totalFees, gasFees)
	totalFees.Add(totalFees, importFee)
	totalFees.Add(totalFees, actionFees)

	computeSuccess := transactionComputeSucceeded(msgRes)
	actionSuccess := actionPhase != nil && actionPhase.Success
	aborted := skipReason != nil || !(computeSuccess && actionSuccess)
	var bouncePhase *tlb.BouncePhase
	if aborted && transactionShouldBounce(&msg.msg, skipReason, computeSuccess, actionBounce) {
		bounceRes, bounceErr := transactionPrepareBouncePhase(&msg.msg, finalBalance, nextExtraCurrencies, msgBalanceRemaining, gasFees, actionFine, startLT, now, len(outMessages), blockchainCfg, skipReason, msgRes, actionPhase)
		if bounceErr != nil {
			return nil, bounceErr
		}
		if bounceRes != nil {
			bouncePhase = bounceRes.phase
			finalBalance = bounceRes.balance
			nextExtraCurrencies = bounceRes.extraCurrencies
			totalFees.Add(totalFees, bounceRes.msgFees)
			if bounceRes.outMsg != nil {
				outMessages = append(outMessages, OutMessage{Cell: bounceRes.outMsg, Msg: bounceRes.outMsgParsed})
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
		prepared.destroyed = true
	}
	finalStatus := transactionFinalizeAccountStatus(prepared.status, prepared.deleted, finalBalance, nextExtraCurrencies, accountActivated)
	nextStateHash := runtimeAcc.stateHash
	if finalStatus == tlb.AccountStatusFrozen && (runtimeAcc.status == tlb.AccountStatusActive || accountActivated) {
		nextStateHash = nil
	}
	accountStatus := finalStatus
	finalStatus, accountStatus, nextStateHash, err = transactionNormalizeFrozenFinalState(computeAcc, finalStatus, nextCode, nextData, nextLibraries, nextStateHash, blockchainCfg)
	if err != nil {
		return nil, err
	}
	nextAccount, err := buildTransactionAccountCell(computeAcc, accountStatus, finalBalance, nextExtraCurrencies, endLT, prepared.lastPaid, prepared.duePayment, nextCode, nextData, nextLibraries, nextStateHash, blockchainCfg, opts.AccountStorageStat)
	if err != nil {
		return nil, err
	}

	txAccountAddr, err := transactionAccountIDAddr(runtimeAcc.addr)
	if err != nil {
		return nil, err
	}
	txCell, err := buildTransactionCell(transactionBuildParams{
		accountAddr: txAccountAddr,
		startLT:     startLT,
		prevTxHash:  runtimeAcc.prevTxHash,
		prevTxLT:    runtimeAcc.prevTxLT,
		now:         now,
		origStatus:  runtimeAcc.status,
		endStatus:   finalStatus,
		inMsg:       msg.cell,
		outMsgs:     outMessages,
		totalFees:   totalFees,
		oldHash:     runtimeAcc.originalCell.Hash(),
		newHash:     nextAccount.cell.Hash(),
		description: transactionBuildDescriptionParams{
			msg:           &msg.msg,
			creditFirst:   prepared.creditFirst,
			storagePhase:  prepared.storagePhase,
			creditPhase:   prepared.creditPhase,
			computeResult: msgRes,
			computeGas:    gas,
			gasFees:       gasFees,
			actionPhase:   actionPhase,
			bouncePhase:   bouncePhase,
			skipReason:    skipReason,
			msgStateUsed:  msgStateUsed && runtimeAcc.status != tlb.AccountStatusActive,
			activated:     accountActivated && runtimeAcc.status != tlb.AccountStatusActive,
			destroyed:     prepared.destroyed,
		},
	})
	if err != nil {
		return nil, err
	}

	return out, fillTransactionExecutionResult(out, txCell, acc, nextAccount, outMessages, startLT, endLT)
}

// EmulateTickTockTransaction executes a tick or tock system transaction of a
// masterchain special account.
func (tvm *TVM) EmulateTickTockTransaction(block *BlockContext, acc *PreparedAccount, isTock bool, opts TransactionOptions) (*TransactionExecutionResult, error) {
	if block == nil {
		return nil, errors.New("block context is required")
	}
	if acc == nil {
		return nil, errors.New("prepared account is required")
	}
	blockchainCfg := block.cfg

	runtimeAcc, proof, err := acc.runtimeForExecution(opts.BuildProof)
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

	now := block.now
	isSpecial := blockchainCfg.isSpecialAccount(runtimeAcc.addr)
	runtimeAcc.isSpecial = isSpecial
	storageDueLimits := blockchainCfg.storageDueLimitsFor(transactionIsMasterchain(runtimeAcc.addr))
	storageFee, err := transactionComputeStorageFee(blockchainCfg, runtimeAcc, now)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		storageFee = transactionCoinsNano(runtimeAcc.storageInfo.DuePayment)
	}

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
	prepared.applyStoragePhase(runtimeAcc, storageFee, now, blockchainCfg.globalVersion(), storageDueLimits, false)
	if isSpecial {
		prepared.lastPaid = 0
	}

	startLT := transactionStartLT(runtimeAcc.storageLT, transactionExecutionLogicalTime(runtimeAcc.prevTxLT, opts.LogicalTime), nil)
	env := newTransactionExecEnv(block, &opts, runtimeAcc, nil, nil, prepared, startLT)
	env.proof = proof

	var skipReason *tlb.ComputeSkipReason
	gas := transactionTickTockGas(opts.Gas, now, blockchainCfg, runtimeAcc.addr, prepared.balance, isSpecial)
	if prepared.balance.Sign() <= 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	} else if prepared.status != tlb.AccountStatusActive || prepared.deleted || runtimeAcc.code == nil {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}
	} else if gas.Limit == 0 && gas.Credit == 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	}

	var msgRes *MessageExecutionResult
	if skipReason == nil {
		msgRes, err = tvm.executeTickTockTransaction(runtimeAcc, isTock, env, gas)
		if err != nil {
			return nil, err
		}
		transactionNormalizeGasUsage(msgRes)
		if err = transactionApplyPrecompiledGasUsage(msgRes, env.precompiledGasUsage); err != nil {
			return nil, err
		}
	}

	out := &TransactionExecutionResult{}
	if msgRes != nil {
		out.ExecutionResult = msgRes.ExecutionResult
		out.Accepted = msgRes.Accepted
	}

	endLT := startLT + 1
	nextCode := runtimeAcc.code
	nextData := runtimeAcc.data
	nextLibraries := runtimeAcc.libraries
	nextExtraCurrencies := prepared.extraCurrencies
	var outMessages []OutMessage
	var actionPhase *tlb.ActionPhase
	var actionDeleted bool
	gasFees := big.NewInt(0)
	if msgRes != nil && msgRes.Accepted && !isSpecial {
		gasFees = transactionComputeGasFee(blockchainCfg, runtimeAcc.addr, uint64(msgRes.GasUsed))
	}
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
		prepared.destroyed = true
	}
	finalStatus := transactionFinalizeAccountStatus(prepared.status, prepared.deleted, finalBalance, nextExtraCurrencies, false)
	nextStateHash := runtimeAcc.stateHash
	if finalStatus == tlb.AccountStatusFrozen && runtimeAcc.status == tlb.AccountStatusActive {
		nextStateHash = nil
	}
	accountStatus := finalStatus
	finalStatus, accountStatus, nextStateHash, err = transactionNormalizeFrozenFinalState(runtimeAcc, finalStatus, nextCode, nextData, nextLibraries, nextStateHash, blockchainCfg)
	if err != nil {
		return nil, err
	}
	nextAccount, err := buildTransactionAccountCell(runtimeAcc, accountStatus, finalBalance, nextExtraCurrencies, endLT, prepared.lastPaid, prepared.duePayment, nextCode, nextData, nextLibraries, nextStateHash, blockchainCfg, opts.AccountStorageStat)
	if err != nil {
		return nil, err
	}

	txAccountAddr, err := transactionAccountIDAddr(runtimeAcc.addr)
	if err != nil {
		return nil, err
	}
	txCell, err := buildTransactionCell(transactionBuildParams{
		accountAddr: txAccountAddr,
		startLT:     startLT,
		prevTxHash:  runtimeAcc.prevTxHash,
		prevTxLT:    runtimeAcc.prevTxLT,
		now:         now,
		origStatus:  runtimeAcc.status,
		endStatus:   finalStatus,
		outMsgs:     outMessages,
		totalFees:   totalFees,
		oldHash:     runtimeAcc.originalCell.Hash(),
		newHash:     nextAccount.cell.Hash(),
		description: transactionBuildDescriptionParams{
			tickTock:      true,
			isTock:        isTock,
			storagePhase:  prepared.storagePhase,
			computeResult: msgRes,
			computeGas:    gas,
			gasFees:       gasFees,
			actionPhase:   actionPhase,
			skipReason:    skipReason,
			destroyed:     prepared.destroyed,
		},
	})
	if err != nil {
		return nil, err
	}

	return out, fillTransactionExecutionResult(out, txCell, acc, nextAccount, outMessages, startLT, endLT)
}

func transactionNormalizeGasUsage(res *MessageExecutionResult) {
	if res.GasUsed > res.Gas.Base {
		res.GasUsed = res.Gas.Base
	}
}

func transactionNoCodeExecutionResult(code, data *cell.Cell, gas vm.Gas) *MessageExecutionResult {
	return &MessageExecutionResult{
		ExecutionResult: ExecutionResult{
			ExitCode: -vmerr.CodeOutOfGas,
			Gas:      gas,
			Code:     code,
			Data:     data,
		},
		Accepted: gas.Credit == 0,
	}
}

func transactionApplyPrecompiledGasUsage(res *MessageExecutionResult, value *big.Int) error {
	precompiledGas, ok, err := transactionPrecompiledGasUsage(value)
	if err != nil || !ok {
		return err
	}
	if res.ExitCode == ^int64(vmerr.CodeOutOfGas) {
		return nil
	}

	res.GasUsed = precompiledGas
	res.Steps = 0
	return nil
}

func (tvm *TVM) executeTransactionMessage(acc *transactionRuntimeAccount, env *transactionExecEnv, gas vm.Gas, msgBalance *big.Int) (*MessageExecutionResult, error) {
	msg := env.msg
	body := messageBodyCell(msg.Msg.Payload())
	stack := vm.NewStack()
	balance := new(big.Int).Set(env.balance)

	switch msg.MsgType {
	case tlb.MsgTypeExternalIn:
		if err := stack.PushOwnedInt(balance); err != nil {
			return nil, err
		}
		if err := stack.PushSmallInt(0); err != nil {
			return nil, err
		}
		if err := stack.PushCell(env.msgCell); err != nil {
			return nil, err
		}
		bodySlice, err := body.BeginParse()
		if err != nil {
			return nil, err
		}
		if err := stack.PushOwnedSlice(bodySlice); err != nil {
			return nil, err
		}
		if err := stack.PushSmallInt(-1); err != nil {
			return nil, err
		}
	case tlb.MsgTypeInternal:
		if err := stack.PushOwnedInt(balance); err != nil {
			return nil, err
		}
		if err := stack.PushOwnedInt(transactionBigOrZero(msgBalance)); err != nil {
			return nil, err
		}
		if err := stack.PushCell(env.msgCell); err != nil {
			return nil, err
		}
		bodySlice, err := body.BeginParse()
		if err != nil {
			return nil, err
		}
		if err := stack.PushOwnedSlice(bodySlice); err != nil {
			return nil, err
		}
		if err := stack.PushSmallInt(0); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported input message type %s", msg.MsgType)
	}

	if acc.code == nil {
		return transactionNoCodeExecutionResult(acc.code, acc.data, gas), nil
	}
	c7, err := env.buildC7(acc.code, balance)
	if err != nil {
		return nil, err
	}

	libraries := transactionExecutionLibraries(acc, env.block.libraries, env.cfg.version)
	return tvm.executeMessageEmulation(acc.code, acc.data, c7, gas, stack, env.stopOnAccept, env.opts.SignatureCheckAlwaysSucceed, env.proof, env.opts.TraceHook, env.cfg, libraries...)
}

func (tvm *TVM) executeTickTockTransaction(acc *transactionRuntimeAccount, isTock bool, env *transactionExecEnv, gas vm.Gas) (*MessageExecutionResult, error) {
	accAddr, err := messageEmulationAccountAddr(acc.addr)
	if err != nil {
		return nil, err
	}

	stack := vm.NewStack()
	balance := new(big.Int).Set(env.balance)
	if err = stack.PushOwnedInt(balance); err != nil {
		return nil, err
	}
	if err = stack.PushOwnedInt(accAddr); err != nil {
		return nil, err
	}
	if err = stack.PushBool(isTock); err != nil {
		return nil, err
	}
	if err = stack.PushSmallInt(-2); err != nil {
		return nil, err
	}

	c7, err := env.buildC7(acc.code, balance)
	if err != nil {
		return nil, err
	}

	libraries := transactionExecutionLibraries(acc, env.block.libraries, env.cfg.version)
	return tvm.executeMessageEmulation(acc.code, acc.data, c7, gas, stack, false, env.opts.SignatureCheckAlwaysSucceed, env.proof, env.opts.TraceHook, env.cfg, libraries...)
}

const transactionLTAlignment = uint64(1_000_000)

func transactionExecutionLogicalTime(prevTxLT uint64, configured int64) int64 {
	if configured > 0 {
		return configured
	}
	base := prevTxLT/transactionLTAlignment + 1
	if base == 0 {
		base = 1
	}
	return int64(base * transactionLTAlignment)
}

func transactionBlockLogicalTime(startLT uint64) int64 {
	return int64(startLT - startLT%transactionLTAlignment)
}

func transactionInt64OrZero(v *big.Int) int64 {
	if v == nil || !v.IsInt64() {
		return 0
	}
	return v.Int64()
}

func (c *transactionCurrencyBalance) asTuple() tuple.Tuple {
	if c == nil {
		return tuple.NewTupleValue(big.NewInt(0), nil)
	}
	extra, err := c.extraDict()
	if err != nil || extra == nil || extra.IsEmpty() {
		return tuple.NewTupleValue(transactionBigOrZero(c.grams), nil)
	}
	return tuple.NewTupleValue(transactionBigOrZero(c.grams), extra.AsCell())
}

func transactionBuildInMsgParams(msg *tlb.Message, msgBalance *transactionCurrencyBalance) tuple.Tuple {
	if msg == nil {
		return messageInMsgParams(tuple.Tuple{})
	}

	stateInitCell := transactionMaybeStateInitCell(transactionMessageStateInit(msg))
	value := transactionBigOrZero(msgBalance.grams)
	valueExtra := transactionCurrencyExtraCell(msgBalance)
	switch msg.MsgType {
	case tlb.MsgTypeInternal:
		in := msg.AsInternal()
		return tuple.NewTupleValue(
			messageTupleBool(in.Bounce),
			messageTupleBool(in.Bounced),
			cell.BeginCell().MustStoreAddr(in.SrcAddr).ToSlice(),
			transactionBigOrZero(in.FwdFee.Nano()),
			messageTupleUint(in.CreatedLT),
			messageTupleUint(uint64(in.CreatedAt)),
			transactionBigOrZero(in.Amount.Nano()),
			value,
			valueExtra,
			stateInitCell,
		)
	case tlb.MsgTypeExternalIn:
		in := msg.AsExternalIn()
		return tuple.NewTupleValue(
			messageTupleInt(0),
			messageTupleInt(0),
			cell.BeginCell().MustStoreAddr(in.SrcAddr).ToSlice(),
			messageTupleInt(0),
			messageTupleInt(0),
			messageTupleInt(0),
			messageTupleInt(0),
			value,
			valueExtra,
			stateInitCell,
		)
	default:
		return messageInMsgParams(tuple.Tuple{})
	}
}

func messageTupleBool(v bool) *big.Int {
	if v {
		return big.NewInt(-1)
	}
	return big.NewInt(0)
}

func transactionCurrencyExtraCell(value *transactionCurrencyBalance) *cell.Cell {
	if value == nil {
		return nil
	}
	extra, err := value.extraDict()
	if err != nil || extra == nil || extra.IsEmpty() {
		return nil
	}
	return extra.AsCell()
}

func transactionMaybeStateInitCell(stateInit *tlb.StateInit) *cell.Cell {
	if stateInit == nil {
		return nil
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		return nil
	}
	return stateCell
}

// accountRandSeedBytes derives the per-account c7 rand seed from the block
// seed, using the pre-v8 layout quirk of the reference implementation when
// needed.
func accountRandSeedBytes(blockSeed []byte, addr *address.Address, globalVersion uint32) ([]byte, error) {
	if len(blockSeed) == 0 {
		return nil, nil
	}
	addrData, err := transactionRewrittenAccountAddressData(addr)
	if err != nil {
		return nil, err
	}

	h := sha256.New()
	h.Write(transactionBits256(blockSeed))
	if globalVersion < 8 {
		h.Write(addrData[:4])
		h.Write(addrData[:28])
	} else {
		h.Write(addrData)
	}
	return h.Sum(nil), nil
}

func transactionRewrittenAccountAddressData(addr *address.Address) ([]byte, error) {
	if addr == nil || addr.Type() != address.StdAddress || len(addr.Data()) != 32 {
		return nil, errors.New("transaction rand seed requires std 256-bit account address")
	}

	data := append([]byte(nil), addr.Data()...)
	anycast := addr.Anycast()
	if anycast == nil {
		return data, nil
	}

	depth := anycast.Depth()
	prefix := anycast.Prefix()
	if depth == 0 || depth > 30 || uint(len(prefix)*8) < depth {
		return nil, errors.New("transaction rand seed requires valid account anycast")
	}
	for i := uint(0); i < depth; i++ {
		mask := byte(1 << (7 - i%8))
		if transactionBit(prefix, int(i)) == 1 {
			data[i/8] |= mask
		} else {
			data[i/8] &^= mask
		}
	}
	return data, nil
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
