package tvm

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
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
	if err := transactionValidateMessageStateInitLibs(&msg); err != nil {
		return nil, err
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

	blockchainCfg := newTransactionConfig(cfg.ConfigRoot)
	if err = transactionValidateInboundExternalMessage(msgCell, &msg, blockchainCfg); err != nil {
		return nil, err
	}
	isSpecial := transactionIsSpecialAccount(blockchainCfg, runtimeAcc.addr)
	runtimeAcc.isSpecial = isSpecial

	storageDueLimits := transactionGetStorageDueLimits(blockchainCfg, runtimeAcc.addr)

	storageFee := big.NewInt(0)
	storageFee, err = transactionComputeStorageFee(blockchainCfg, runtimeAcc, now)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		storageFee = transactionCoinsNano(runtimeAcc.storageInfo.DuePayment)
	}
	importFee := big.NewInt(0)
	if !isSpecial {
		importFee, err = transactionComputeImportFee(blockchainCfg, runtimeAcc.addr, &msg, msgCell)
		if err != nil {
			return nil, err
		}
	}
	prepared, err := transactionPrepareInitialPhases(runtimeAcc, &msg, storageFee, importFee, now, blockchainCfg, storageDueLimits)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		prepared.lastPaid = 0
	}

	execCfg := cfg
	execCfg.Address = runtimeAcc.addr
	execCfg.Now = now
	startLT := transactionStartLT(runtimeAcc.storageLT, transactionExecutionLogicalTime(runtimeAcc.prevTxLT, execCfg.LogicalTime), &msg)
	explicitC7 := transactionHasExplicitC7Context(execCfg)
	transactionPrepareExecutionConfig(&execCfg, runtimeAcc, &msg, prepared, startLT)
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
			addressSuspended := false
			if !prepared.deleted && (prepared.status == tlb.AccountStatusUninit || prepared.status == tlb.AccountStatusNonExist) && transactionMessageStateInit(&msg) != nil {
				addressSuspended, err = transactionIsAddressSuspended(blockchainCfg, now, runtimeAcc.addr)
				if err != nil {
					return nil, fmt.Errorf("check suspended address: %w", err)
				}
			}

			computeAcc, msgStateUsed, skipReason, err = transactionPrepareComputeAccount(runtimeAcc, prepared.status, prepared.deleted, &msg, addressSuspended, blockchainCfg)
			if err != nil {
				return nil, err
			}
			if cfg.BuildProof && msgStateUsed && skipReason == nil {
				return nil, errors.New("account execution proof cannot be built for code loaded from message state init")
			}
			if skipReason == nil {
				var precompiledUsage *big.Int
				gas, precompiledUsage, skipReason, err = transactionApplyPrecompiledGasConfig(cfg, blockchainCfg, computeAcc.code, gas)
				if err != nil {
					return nil, err
				}
				if precompiledUsage != nil {
					execCfg.PrecompiledGasUsage = precompiledUsage
				}
			}
		}
	}

	var msgRes *MessageExecutionResult
	accountActivated := false
	if skipReason == nil {
		globalVersion, err := transactionExecutionGlobalVersion(execCfg, blockchainCfg, tvm.globalVersion)
		if err != nil {
			return nil, err
		}
		msgRes, err = tvm.executeTransactionMessage(computeAcc, msgCell, &msg, execCfg, gas, prepared.msgBalance.grams, proof, explicitC7, globalVersion)
		if err != nil {
			return nil, err
		}
		transactionNormalizeGasUsage(msgRes)
		if err = transactionApplyPrecompiledGasUsage(msgRes, execCfg.PrecompiledGasUsage); err != nil {
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
	if msg.MsgType == tlb.MsgTypeExternalIn && (msgRes == nil || !msgRes.Accepted) {
		return out, nil
	}

	endLT := startLT + 1
	nextCode := computeAcc.code
	nextData := computeAcc.data
	nextLibraries := computeAcc.libraries
	nextExtraCurrencies := prepared.extraCurrencies
	msgBalanceRemaining := prepared.msgBalance
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
	if aborted && transactionShouldBounce(&msg, skipReason, computeSuccess, actionBounce) {
		bounceRes, bounceErr := transactionPrepareBouncePhase(&msg, finalBalance, nextExtraCurrencies, msgBalanceRemaining, gasFees, actionFine, startLT, now, len(outMessages), blockchainCfg, skipReason, msgRes, actionPhase)
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
	nextAccountCell, nextAccountState, nextAccountStorageStat, err := buildTransactionAccountCell(computeAcc, accountStatus, finalBalance, nextExtraCurrencies, endLT, prepared.lastPaid, prepared.duePayment, nextCode, nextData, nextLibraries, nextStateHash, blockchainCfg, cfg.AccountStorageStat)
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
			msgStateUsed:  msgStateUsed && runtimeAcc.status != tlb.AccountStatusActive,
			activated:     accountActivated && runtimeAcc.status != tlb.AccountStatusActive,
			destroyed:     prepared.destroyed,
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

	blockchainCfg := newTransactionConfig(cfg.ConfigRoot)
	isSpecial := transactionIsSpecialAccount(blockchainCfg, runtimeAcc.addr)
	runtimeAcc.isSpecial = isSpecial
	storageDueLimits := transactionGetStorageDueLimits(blockchainCfg, runtimeAcc.addr)
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

	execCfg := cfg
	execCfg.Address = runtimeAcc.addr
	execCfg.Now = now
	startLT := transactionStartLT(runtimeAcc.storageLT, transactionExecutionLogicalTime(runtimeAcc.prevTxLT, execCfg.LogicalTime), nil)
	explicitC7 := transactionHasExplicitC7Context(execCfg)
	transactionPrepareExecutionConfig(&execCfg, runtimeAcc, nil, prepared, startLT)
	execCfg.Balance = new(big.Int).Set(prepared.balance)

	var skipReason *tlb.ComputeSkipReason
	gas := transactionTickTockGas(execCfg, blockchainCfg, runtimeAcc.addr, prepared.balance, isSpecial)
	if prepared.balance.Sign() <= 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	} else if prepared.status != tlb.AccountStatusActive || prepared.deleted || runtimeAcc.code == nil {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}
	} else if gas.Limit == 0 && gas.Credit == 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	}

	var msgRes *MessageExecutionResult
	if skipReason == nil {
		globalVersion, err := transactionExecutionGlobalVersion(execCfg, blockchainCfg, tvm.globalVersion)
		if err != nil {
			return nil, err
		}
		msgRes, err = tvm.executeTickTockTransaction(runtimeAcc, isTock, execCfg, gas, proof, explicitC7, globalVersion)
		if err != nil {
			return nil, err
		}
		transactionNormalizeGasUsage(msgRes)
		if err = transactionApplyPrecompiledGasUsage(msgRes, execCfg.PrecompiledGasUsage); err != nil {
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
	var outMessages []*cell.Cell
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
	nextAccountCell, nextAccountState, nextAccountStorageStat, err := buildTransactionAccountCell(runtimeAcc, accountStatus, finalBalance, nextExtraCurrencies, endLT, prepared.lastPaid, prepared.duePayment, nextCode, nextData, nextLibraries, nextStateHash, blockchainCfg, cfg.AccountStorageStat)
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
			destroyed:     prepared.destroyed,
		},
	})
	if err != nil {
		return nil, err
	}

	return out, fillTransactionExecutionResult(out, txCell, nextAccountCell, nextAccountState, nextAccountStorageStat, startLT)
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

func (tvm *TVM) executeTransactionMessage(acc *transactionRuntimeAccount, msgCell *cell.Cell, msg *tlb.Message, cfg TransactionEmulationConfig, gas vm.Gas, msgBalance *big.Int, proof *cell.MerkleProofBuilder, explicitC7 bool, globalVersion int) (*MessageExecutionResult, error) {
	body := messageBodyCell(msg.Msg.Payload())
	stack := vm.NewStack()
	balance := new(big.Int).Set(cfg.Balance)

	switch msg.MsgType {
	case tlb.MsgTypeExternalIn:
		if err := stack.PushOwnedInt(balance); err != nil {
			return nil, err
		}
		if err := stack.PushSmallInt(0); err != nil {
			return nil, err
		}
		if err := stack.PushCell(msgCell); err != nil {
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
		if err := stack.PushCell(msgCell); err != nil {
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
	c7, err := buildTransactionEmulationC7(acc.addr, acc.code, cfg, balance, uint32(globalVersion), explicitC7)
	if err != nil {
		return nil, err
	}

	libraries := transactionExecutionLibraries(acc, cfg, explicitC7)

	res, err := tvm.executeMessageEmulation(acc.code, acc.data, c7, gas, stack, cfg.StopOnAccept, cfg.ChksigAlwaysSucceed, proof, cfg.TraceHook, globalVersion, libraries...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (tvm *TVM) executeTickTockTransaction(acc *transactionRuntimeAccount, isTock bool, cfg TransactionEmulationConfig, gas vm.Gas, proof *cell.MerkleProofBuilder, explicitC7 bool, globalVersion int) (*MessageExecutionResult, error) {
	accAddr, err := messageEmulationAccountAddr(acc.addr)
	if err != nil {
		return nil, err
	}

	stack := vm.NewStack()
	balance := new(big.Int).Set(cfg.Balance)
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

	c7, err := buildTransactionEmulationC7(acc.addr, acc.code, cfg, balance, uint32(globalVersion), explicitC7)
	if err != nil {
		return nil, err
	}

	libraries := transactionExecutionLibraries(acc, cfg, explicitC7)

	res, err := tvm.executeMessageEmulation(acc.code, acc.data, c7, gas, stack, cfg.StopOnAccept, cfg.ChksigAlwaysSucceed, proof, cfg.TraceHook, globalVersion, libraries...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func transactionExecutionLibraries(acc *transactionRuntimeAccount, cfg TransactionEmulationConfig, explicitC7 bool) []*cell.Cell {
	if explicitC7 {
		if len(cfg.Libraries) == 0 {
			return nil
		}
		return cfg.Libraries
	}

	var inMsgLibrary, accountLibrary *cell.Cell
	if acc.inMsgLibraries != nil {
		inMsgLibrary = acc.inMsgLibraries.AsCell()
	}
	if acc.libraries != nil {
		accountLibrary = acc.libraries.AsCell()
	}
	if inMsgLibrary == nil && accountLibrary == nil && len(cfg.Libraries) > 0 {
		return cfg.Libraries
	}

	librariesNum := len(cfg.Libraries)
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
	copy(libraries[idx:], cfg.Libraries)
	return libraries
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

func transactionPrepareExecutionConfig(cfg *TransactionEmulationConfig, acc *transactionRuntimeAccount, msg *tlb.Message, prepared *transactionPreparedPhases, startLT uint64) {
	cfg.LogicalTime = int64(startLT)
	if cfg.BlockLT == 0 {
		cfg.BlockLT = transactionBlockLogicalTime(startLT)
	}
	if prepared == nil {
		return
	}
	if transactionHasExplicitC7Context(*cfg) {
		return
	}
	if cfg.IncomingValue.Len() == 0 {
		cfg.IncomingValue = prepared.msgBalance.asTuple()
	}
	if cfg.StorageFees == 0 {
		cfg.StorageFees = transactionInt64OrZero(prepared.storagePhase.StorageFeesCollected.Nano())
	}
	if cfg.DuePayment == nil {
		cfg.DuePayment = transactionBigOrZero(transactionCoinsNano(prepared.duePayment))
	}
	if cfg.InMsgParams.Len() == 0 {
		cfg.InMsgParams = transactionBuildInMsgParams(msg, prepared.msgBalance)
	}
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

func buildTransactionEmulationC7(addr *address.Address, code *cell.Cell, cfg MessageEmulationConfig, balance *big.Int, globalVersion uint32, explicitC7 bool) (tuple.Tuple, error) {
	seed, err := transactionEmulationSeedForConfig(cfg, addr, globalVersion, explicitC7)
	if err != nil {
		return tuple.Tuple{}, err
	}
	return buildEmulationC7(addr, code, cfg, balance, seed, globalVersion)
}

func transactionEmulationSeedForConfig(cfg MessageEmulationConfig, addr *address.Address, globalVersion uint32, explicitC7 bool) (*big.Int, error) {
	if explicitC7 {
		return messageEmulationSeed(cfg.RandSeed)
	}
	return transactionEmulationSeed(cfg.RandSeed, addr, globalVersion)
}

func transactionHasExplicitC7Context(cfg MessageEmulationConfig) bool {
	return cfg.IncomingValue.Len() > 0 ||
		cfg.InMsgParams.Len() > 0 ||
		cfg.UnpackedConfig.Len() > 0 ||
		cfg.PrevBlocks != nil
}

func transactionEmulationSeed(blockSeed []byte, addr *address.Address, globalVersion uint32) (*big.Int, error) {
	if len(blockSeed) == 0 {
		return big.NewInt(0), nil
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
		return new(big.Int).SetBytes(h.Sum(nil)), nil
	}
	h.Write(addrData)
	return new(big.Int).SetBytes(h.Sum(nil)), nil
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
