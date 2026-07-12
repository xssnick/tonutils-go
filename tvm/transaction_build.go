package tvm

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type transactionBuildParams struct {
	accountAddr *address.Address
	startLT     uint64
	prevTxHash  []byte
	prevTxLT    uint64
	now         uint32
	origStatus  tlb.AccountStatus
	endStatus   tlb.AccountStatus
	inMsg       *cell.Cell
	outMsgs     []OutMessage
	totalFees   *big.Int
	oldHash     []byte
	newHash     []byte
	description transactionBuildDescriptionParams
}

type transactionBuildDescriptionParams struct {
	tickTock      bool
	isTock        bool
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
	for i, outMsg := range params.outMsgs {
		key := cell.BeginCell().MustStoreUInt(uint64(i), 15).EndCell()
		value := cell.BeginCell().MustStoreRef(outMsg.Cell)
		if err := outDict.SetBuilder(key, value); err != nil {
			return nil, fmt.Errorf("failed to store outbound message %d: %w", i, err)
		}
	}

	ioCell := buildTransactionIOCell(params.inMsg, outDict)
	stateUpdateCell := buildTransactionHashUpdateCell(params.oldHash, params.newHash)
	descriptionCell, err := buildTransactionDescription(params.description).ToCell()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize transaction description: %w", err)
	}

	builder := cell.BeginCell().
		MustStoreUInt(0b0111, 4).
		MustStoreSlice(params.accountAddr.Data(), 256).
		MustStoreUInt(params.startLT, 64).
		MustStoreSlice(transactionBits256(params.prevTxHash), 256).
		MustStoreUInt(params.prevTxLT, 64).
		MustStoreUInt(uint64(params.now), 32).
		MustStoreUInt(uint64(len(params.outMsgs)), 15)
	if err = storeTransactionAccountStatus(builder, params.origStatus); err != nil {
		return nil, err
	}
	if err = storeTransactionAccountStatus(builder, params.endStatus); err != nil {
		return nil, err
	}
	builder.MustStoreRef(ioCell)
	if err = storeTransactionCurrencyCollection(builder, params.totalFees, nil); err != nil {
		return nil, err
	}
	return builder.
		MustStoreRef(stateUpdateCell).
		MustStoreRef(descriptionCell).
		EndCell(), nil
}

func fillTransactionExecutionResult(out *TransactionExecutionResult, txCell *cell.Cell, prev *PreparedAccount, next *builtTransactionAccount, outMessages []OutMessage, startLT, endLT uint64) error {
	nextShard := &tlb.ShardAccount{
		Account:       next.cell,
		LastTransHash: append([]byte(nil), txCell.Hash()...),
		LastTransLT:   startLT,
	}
	nextAccount, err := prepareAccountFromState(nextShard, next.state, prev.Address(), next.storageCell, next.storageCellForStat)
	if err != nil {
		return fmt.Errorf("failed to prepare next account state: %w", err)
	}

	out.TransactionCell = txCell
	out.NextAccount = nextAccount
	out.OutMessages = outMessages
	out.EndLT = endLT
	out.AccountStorageStat = next.storageStat
	return nil
}

func buildTransactionIOCell(inMsg *cell.Cell, outDict *cell.Dictionary) *cell.Cell {
	builder := cell.BeginCell()
	if inMsg == nil {
		builder.MustStoreBoolBit(false)
	} else {
		builder.MustStoreMaybeRef(inMsg)
	}
	return builder.MustStoreDict(outDict).EndCell()
}

func transactionBits256(src []byte) []byte {
	if len(src) == 32 {
		return src
	}
	out := make([]byte, 32)
	if len(src) >= 32 {
		copy(out, src[len(src)-32:])
		return out
	}
	copy(out[32-len(src):], src)
	return out
}

func buildTransactionDescription(params transactionBuildDescriptionParams) tlb.Marshaller {
	if params.tickTock {
		storagePhase := tlb.StoragePhase{}
		if params.storagePhase != nil {
			storagePhase = *params.storagePhase
		}

		computePhase := buildTransactionComputePhase(params)
		computeSuccess := transactionComputeSucceeded(params.computeResult)
		actionSuccess := params.actionPhase != nil && params.actionPhase.Success
		actionPhase := params.actionPhase
		if !computeSuccess {
			actionPhase = nil
		}

		return tlb.TransactionDescriptionTickTock{
			IsTock:       params.isTock,
			StoragePhase: storagePhase,
			ComputePhase: computePhase,
			ActionPhase:  actionPhase,
			Aborted:      !(computeSuccess && actionSuccess),
			Destroyed:    params.destroyed,
		}
	}

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

	computePhase := buildTransactionComputePhase(params)
	computeSuccess := transactionComputeSucceeded(params.computeResult)
	actionSuccess := params.actionPhase != nil && params.actionPhase.Success

	return tlb.TransactionDescriptionOrdinary{
		CreditFirst:  params.creditFirst,
		StoragePhase: params.storagePhase,
		CreditPhase:  params.creditPhase,
		ComputePhase: computePhase,
		ActionPhase:  params.actionPhase,
		Aborted:      !(computeSuccess && actionSuccess),
		BouncePhase:  params.bouncePhase,
		Destroyed:    params.destroyed,
	}
}

func buildTransactionComputePhase(params transactionBuildDescriptionParams) tlb.ComputePhase {
	if params.skipReason != nil {
		return tlb.ComputePhase{
			Phase: tlb.ComputePhaseSkipped{
				Reason: *params.skipReason,
			},
		}
	}

	gasLimit := params.computeGas.Limit
	if params.msg != nil && params.msg.MsgType == tlb.MsgTypeExternalIn {
		gasLimit = 0
	}

	var gasCredit *big.Int
	if params.computeGas.Credit > 0 {
		gasCredit = big.NewInt(params.computeGas.Credit)
	}

	computeSuccess := transactionComputeSucceeded(params.computeResult)

	return tlb.ComputePhase{
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
	}
}

func transactionComputeSucceeded(res *MessageExecutionResult) bool {
	return res != nil && res.Accepted && res.Committed
}

func transactionComputeExitArg(res *MessageExecutionResult) *int32 {
	if res == nil || transactionComputeSucceeded(res) || res.Stack == nil || res.Stack.Len() == 0 {
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
