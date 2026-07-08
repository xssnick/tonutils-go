package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionBounceMessageUsageCountsBodyRefsOnly(t *testing.T) {
	empty, err := transactionBounceMessageUsage(nil, nil, transactionTestConfigWithGlobalVersion(t, 13))
	if err != nil {
		t.Fatal(err)
	}
	if empty != (transactionUsage{}) {
		t.Fatalf("nil bounce message usage = %+v, want empty", empty)
	}

	leaf := cell.BeginCell().MustStoreUInt(0x3, 2).EndCell()
	refA := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	refB := cell.BeginCell().MustStoreUInt(0xB, 4).MustStoreRef(leaf).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xFFFF, 16).
		MustStoreRef(refA).
		MustStoreRef(refB).
		EndCell()

	got, err := transactionBounceMessageUsage(&tlb.InternalMessage{}, body, transactionTestConfigWithGlobalVersion(t, 13))
	if err != nil {
		t.Fatal(err)
	}

	want := transactionUsage{cells: 3, bits: 14}
	if got != want {
		t.Fatalf("bounce message usage = %+v, want %+v", got, want)
	}
}

func TestTransactionBounceMessageUsageExtraCurrenciesBeforeV13(t *testing.T) {
	extra := makeTransactionExtraCurrencies(t, 1, 777)
	want, err := transactionCollectUsage(extra.AsCell())
	if err != nil {
		t.Fatal(err)
	}
	if want.cells == 0 {
		t.Fatal("test extra currency dictionary should use storage cells")
	}

	msg := &tlb.InternalMessage{ExtraCurrencies: extra}
	body := cell.BeginCell().EndCell()

	gotV12, err := transactionBounceMessageUsage(msg, body, transactionTestConfigWithGlobalVersion(t, 12))
	if err != nil {
		t.Fatal(err)
	}
	if gotV12 != want {
		t.Fatalf("v12 bounce message usage = %+v, want %+v", gotV12, want)
	}

	gotV13, err := transactionBounceMessageUsage(msg, body, transactionTestConfigWithGlobalVersion(t, 13))
	if err != nil {
		t.Fatal(err)
	}
	if gotV13 != (transactionUsage{}) {
		t.Fatalf("v13 bounce message usage = %+v, want empty", gotV13)
	}
}

func TestTransactionPrepareBouncePhaseEarlyExitAndNoFunds(t *testing.T) {
	if res, err := transactionPrepareBouncePhase(nil, big.NewInt(0), nil, transactionZeroCurrencyBalance(), big.NewInt(0), big.NewInt(0), 0, 0, 0, emptyPreparedTestConfig(), nil, nil, nil); err != nil || res != nil {
		t.Fatalf("nil message bounce = %+v err=%v, want nil nil", res, err)
	}
	if res, err := transactionPrepareBouncePhase(&tlb.Message{MsgType: tlb.MsgTypeExternalIn, Msg: &tlb.ExternalMessage{}}, big.NewInt(0), nil, transactionZeroCurrencyBalance(), big.NewInt(0), big.NewInt(0), 0, 0, 0, emptyPreparedTestConfig(), nil, nil, nil); err != nil || res != nil {
		t.Fatalf("external message bounce = %+v err=%v, want nil nil", res, err)
	}
	if res, err := transactionPrepareBouncePhase(&tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			SrcAddr: internalEmulationSrcAddr,
			DstAddr: tonopsTestAddr,
		},
	}, big.NewInt(0), nil, transactionZeroCurrencyBalance(), big.NewInt(0), big.NewInt(0), 0, 0, 0, emptyPreparedTestConfig(), nil, nil, nil); err != nil || res != nil {
		t.Fatalf("non-bounce message bounce = %+v err=%v, want nil nil", res, err)
	}
	if res, err := transactionPrepareBouncePhase(&tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			Bounce:  true,
			DstAddr: tonopsTestAddr,
		},
	}, big.NewInt(0), nil, transactionZeroCurrencyBalance(), big.NewInt(0), big.NewInt(0), 0, 0, 0, emptyPreparedTestConfig(), nil, nil, nil); err != nil || res != nil {
		t.Fatalf("invalid source bounce = %+v err=%v, want nil nil", res, err)
	}

	priceCell := buildTransactionMsgForwardPricesCell(t, 1_000, 1<<15)
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 13),
		tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
		tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
	})
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			SrcAddr:     internalEmulationSrcAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(1),
			IHRFee:      tlb.FromNanoTONU(1),
			Body:        cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
		},
	}
	out, err := transactionPrepareBouncePhase(msg, big.NewInt(1), nil, &transactionCurrencyBalance{grams: big.NewInt(1), extra: map[uint32]*big.Int{}}, big.NewInt(0), big.NewInt(0), uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), 0, cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.phase == nil || out.outMsg != nil {
		t.Fatalf("no-funds bounce result = %+v, want phase without out msg", out)
	}
	noFunds, ok := out.phase.Phase.(tlb.BouncePhaseNoFunds)
	if !ok {
		t.Fatalf("bounce phase type = %T, want no-funds", out.phase.Phase)
	}
	if noFunds.ReqFwdFees.Nano().Sign() <= 0 || noFunds.MsgSize.Cells.Sign() == 0 {
		t.Fatalf("no-funds phase = %+v, want positive fee and message size", noFunds)
	}
	if out.balance.Cmp(big.NewInt(1)) != 0 || out.msgFees.Sign() != 0 {
		t.Fatalf("no-funds balance/fees = %s/%s, want unchanged/zero", out.balance, out.msgFees)
	}
}

func TestTransactionBuildBounceBodyLegacyCapabilityAndOriginalLimits(t *testing.T) {
	if body, err := transactionBuildBounceBody(nil, emptyPreparedTestConfig(), nil, nil, nil); err != nil || body.BitsSize() != 0 || body.RefsNum() != 0 {
		t.Fatalf("nil bounce body = %s err=%v, want empty", body.Dump(), err)
	}

	in := &tlb.InternalMessage{
		Body: cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell(),
	}
	if body, err := transactionBuildBounceBody(in, transactionTestConfigWithGlobalVersion(t, 11), nil, nil, nil); err != nil || body.BitsSize() != 0 || body.RefsNum() != 0 {
		t.Fatalf("legacy bounce without capability = %s err=%v, want empty", body.Dump(), err)
	}

	capCfg := transactionTestConfigWithGlobalVersionAndCapabilities(t, 11, 4)
	body, err := transactionBuildBounceBody(&tlb.InternalMessage{}, capCfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if body.BitsSize() != 32 || body.RefsNum() != 0 {
		t.Fatalf("legacy empty-body bounce = bits %d refs %d, want tag only", body.BitsSize(), body.RefsNum())
	}

	largeBody := cell.BeginCell().
		MustStoreSlice(make([]byte, 32), 256).
		MustStoreSlice(make([]byte, 8), 64).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		EndCell()
	body, err = transactionBuildBounceBody(&tlb.InternalMessage{Body: largeBody}, capCfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sl := body.MustBeginParse()
	if tag, err := sl.LoadUInt(32); err != nil || tag != 0xffffffff {
		t.Fatalf("legacy bounce tag = %#x err=%v, want 0xffffffff", tag, err)
	}
	if sl.BitsLeft() != 256 || sl.RefsNum() != 0 {
		t.Fatalf("legacy bounce body tail = bits %d refs %d, want 256 bits and no refs", sl.BitsLeft(), sl.RefsNum())
	}

	original, err := transactionBounceOriginalBody(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if original.BitsSize() != 0 || original.RefsNum() != 0 {
		t.Fatalf("nil original bounce body = %s, want empty", original.Dump())
	}
}

func TestTransactionInboundExtraFlagsBoundaries(t *testing.T) {
	if got := transactionInboundExtraFlags(nil); got != 0 {
		t.Fatalf("nil inbound extra flags = %d, want 0", got)
	}
	if got := transactionInboundExtraFlags(&tlb.InternalMessage{}); got != 0 {
		t.Fatalf("empty inbound extra flags = %d, want 0", got)
	}

	huge := new(big.Int).Lsh(big.NewInt(1), 80)
	if got := transactionInboundExtraFlags(&tlb.InternalMessage{IHRFee: tlb.FromNanoTON(huge)}); got != 0 {
		t.Fatalf("overflow inbound extra flags = %d, want 0", got)
	}
	if got := transactionInboundExtraFlags(&tlb.InternalMessage{IHRFee: tlb.FromNanoTONU(7)}); got != 3 {
		t.Fatalf("masked inbound extra flags = %d, want 3", got)
	}
}

func transactionTestConfigWithGlobalVersionAndCapabilities(t *testing.T, version uint32, capabilities uint64) *PreparedConfig {
	t.Helper()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: version, Capabilities: capabilities})
	if err != nil {
		t.Fatalf("failed to build global version cell: %v", err)
	}
	return transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: versionCell,
	})
}
