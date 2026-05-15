package tvm

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

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
	bounceBody, err := transactionBuildBounceBody(in, cfg, skipReason, computeResult, actionPhase)
	if err != nil {
		return nil, err
	}
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

func transactionBuildBounceBody(in *tlb.InternalMessage, cfg tlb.BlockchainConfig, skipReason *tlb.ComputeSkipReason, computeResult *MessageExecutionResult, actionPhase *tlb.ActionPhase) (*cell.Cell, error) {
	if in == nil {
		return cell.BeginCell().EndCell(), nil
	}

	flags := transactionInboundExtraFlags(in)
	if flags&1 != 0 {
		body := cell.BeginCell().MustStoreUInt(0xFFFFFFFE, 32)
		originalBody, err := transactionBounceOriginalBody(in.Body, flags&2 != 0)
		if err != nil {
			return nil, err
		}
		body.MustStoreRef(originalBody)

		valueCell, err := tlb.ToCell(&tlb.CurrencyCollection{
			Coins:           in.Amount,
			ExtraCurrencies: in.ExtraCurrencies,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to serialize bounced value: %w", err)
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
		return body.EndCell(), nil
	}

	if !transactionHasCapability(cfg, 4) {
		return cell.BeginCell().EndCell(), nil
	}

	body := cell.BeginCell().MustStoreUInt(0xFFFFFFFF, 32)
	if in.Body == nil {
		return body.EndCell(), nil
	}
	bodySlice, err := in.Body.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to load bounced body: %w", err)
	}
	bits := bodySlice.BitsLeft()
	if bits > 256 {
		bits = 256
	}
	if bits > 0 {
		body.MustStoreSlice(bodySlice.MustLoadSlice(bits), bits)
	}
	return body.EndCell(), nil
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

func transactionBounceOriginalBody(body *cell.Cell, full bool) (*cell.Cell, error) {
	if body == nil {
		return cell.BeginCell().EndCell(), nil
	}
	if full {
		return body, nil
	}
	slice, err := body.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to load original body: %w", err)
	}
	b := cell.BeginCell()
	if bits := slice.BitsLeft(); bits > 0 {
		b.MustStoreSlice(slice.MustLoadSlice(bits), bits)
	}
	return b.EndCell(), nil
}

func transactionBouncePhaseExit(skipReason *tlb.ComputeSkipReason, computeResult *MessageExecutionResult, actionPhase *tlb.ActionPhase) (uint8, int32) {
	if skipReason != nil {
		return 0, -transactionSkipReasonCode(skipReason.Type)
	}
	if computeResult != nil && !transactionComputeSucceeded(computeResult) {
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
