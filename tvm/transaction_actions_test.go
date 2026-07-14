package tvm

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTransactionMessageToCellWithRetryKeepsStateInitRefWhenMovingBody(t *testing.T) {
	var attempts []transactionOutboundLayout
	root, used, err := transactionMessageToCellWithRetry(transactionOutboundLayout{}, true, func(layout transactionOutboundLayout) (*cell.Cell, error) {
		attempts = append(attempts, layout)
		if layout.stateInitInRef && layout.bodyInRef {
			return cell.BeginCell().EndCell(), nil
		}
		return nil, errors.New("does not fit")
	})
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if root == nil {
		t.Fatal("retry returned nil cell")
	}
	if !used.stateInitInRef || !used.bodyInRef {
		t.Fatalf("used layout = %+v, want state init and body in refs", used)
	}
	if len(attempts) != 3 {
		t.Fatalf("attempts = %d, want 3", len(attempts))
	}
	if attempts[2] != (transactionOutboundLayout{stateInitInRef: true, bodyInRef: true}) {
		t.Fatalf("final attempt = %+v, want progressive state init/body refs", attempts[2])
	}
}

func TestTransactionMessageToCellWithRetryCanSkipStateInitRef(t *testing.T) {
	var attempts []transactionOutboundLayout
	root, used, err := transactionMessageToCellWithRetry(transactionOutboundLayout{}, false, func(layout transactionOutboundLayout) (*cell.Cell, error) {
		attempts = append(attempts, layout)
		if layout.bodyInRef {
			return cell.BeginCell().EndCell(), nil
		}
		return nil, errors.New("does not fit")
	})
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if root == nil {
		t.Fatal("retry returned nil cell")
	}
	if used.stateInitInRef || !used.bodyInRef {
		t.Fatalf("used layout = %+v, want only body in ref", used)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(attempts))
	}
	if attempts[1] != (transactionOutboundLayout{bodyInRef: true}) {
		t.Fatalf("final attempt = %+v, want body ref only", attempts[1])
	}
}

func TestTransactionNormalizeOutboundMessagePreservesInternalLayout(t *testing.T) {
	state := &tlb.StateInit{
		Code: cell.BeginCell().MustStoreUInt(0xC0, 8).EndCell(),
		Data: cell.BeginCell().MustStoreUInt(0xD0, 8).EndCell(),
	}
	body := cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell()
	dst := address.NewAddressVar(0, tonopsTestAddr.Workchain(), tonopsTestAddr.BitsLen(), tonopsTestAddr.Data())
	layout := transactionOutboundLayout{stateInitInRef: true, bodyInRef: true}

	msgCell, used, err := transactionInternalMessageToCellWithLayout(&tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		Bounced:     true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     dst,
		Amount:      tlb.FromNanoTONU(321),
		StateInit:   state,
		Body:        body,
	}, layout)
	if err != nil {
		t.Fatalf("build outbound internal message: %v", err)
	}
	if used != layout {
		t.Fatalf("used layout = %+v, want %+v", used, layout)
	}
	gotLayout, err := transactionOutboundMessageLayout(msgCell)
	if err != nil {
		t.Fatalf("decode initial layout: %v", err)
	}
	if gotLayout != layout {
		t.Fatalf("initial layout = %+v, want %+v", gotLayout, layout)
	}

	normalized, err := transactionNormalizeOutboundMessage(msgCell, tonopsTestAddr, 123, uint32(tonopsTestTime.Unix()), transactionTestConfigWithGlobalVersion(t, 14))
	if err != nil {
		t.Fatalf("normalize outbound message: %v", err)
	}
	gotLayout, err = transactionOutboundMessageLayout(normalized)
	if err != nil {
		t.Fatalf("decode normalized layout: %v", err)
	}
	if gotLayout != layout {
		t.Fatalf("normalized layout = %+v, want %+v", gotLayout, layout)
	}

	var msg tlb.Message
	if err = transactionParseCell(&msg, normalized); err != nil {
		t.Fatalf("parse normalized message: %v", err)
	}
	intMsg := msg.AsInternal()
	if intMsg.SrcAddr == nil || !intMsg.SrcAddr.Equals(tonopsTestAddr) {
		t.Fatalf("src addr = %v, want %v", intMsg.SrcAddr, tonopsTestAddr)
	}
	if intMsg.Bounced {
		t.Fatal("normalized outbound message should clear bounced flag")
	}
	if intMsg.CreatedLT != 123 || intMsg.CreatedAt != uint32(tonopsTestTime.Unix()) {
		t.Fatalf("created fields = %d/%d", intMsg.CreatedLT, intMsg.CreatedAt)
	}
	if intMsg.DstAddr.Type() != address.StdAddress || !bytes.Equal(intMsg.DstAddr.Data(), tonopsTestAddr.Data()) {
		t.Fatalf("dst addr = %v, want normalized std account address", intMsg.DstAddr)
	}
	if intMsg.StateInit == nil || intMsg.StateInit.Code.HashKey() != state.Code.HashKey() || intMsg.StateInit.Data.HashKey() != state.Data.HashKey() {
		t.Fatal("state init changed during normalization")
	}
	if intMsg.Body == nil || intMsg.Body.HashKey() != body.HashKey() {
		t.Fatal("body changed during normalization")
	}
}

func TestTransactionExternalOutMessageToCellRetriesRefs(t *testing.T) {
	state := &tlb.StateInit{
		Code: cell.BeginCell().MustStoreUInt(0xC0, 8).EndCell(),
		Data: cell.BeginCell().MustStoreUInt(0xD0, 8).EndCell(),
	}
	threeRefBody := cell.BeginCell().
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(2, 2).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(3, 3).EndCell()).
		EndCell()
	extDst := address.NewAddressExt(0, 16, []byte{0xAB, 0xCD})

	out, err := transactionExternalOutMessageToCell(&tlb.ExternalMessageOut{
		SrcAddr:   address.NewAddressNone(),
		DstAddr:   extDst,
		StateInit: state,
		Body:      threeRefBody,
	}, transactionOutboundLayout{})
	if err != nil {
		t.Fatalf("build external out with retry: %v", err)
	}
	layout, err := transactionOutboundMessageLayout(out)
	if err != nil {
		t.Fatalf("decode retried external layout: %v", err)
	}
	if layout != (transactionOutboundLayout{stateInitInRef: true}) {
		t.Fatalf("retried external layout = %+v, want state init ref with inline body", layout)
	}

	largeBody := cell.BeginCell().MustStoreSlice(make([]byte, 128), 1023).EndCell()
	out, err = transactionExternalOutMessageToCell(&tlb.ExternalMessageOut{
		SrcAddr: address.NewAddressNone(),
		DstAddr: extDst,
		Body:    largeBody,
	}, transactionOutboundLayout{})
	if err != nil {
		t.Fatalf("build external out with body retry: %v", err)
	}
	layout, err = transactionOutboundMessageLayout(out)
	if err != nil {
		t.Fatalf("decode body-ref external layout: %v", err)
	}
	if layout != (transactionOutboundLayout{bodyInRef: true}) {
		t.Fatalf("body-ref external layout = %+v, want body ref only", layout)
	}
}

func TestTransactionOutboundMessageLayoutRejectsInvalidInputs(t *testing.T) {
	if _, err := transactionNormalizeOutboundMessage(nil, tonopsTestAddr, 1, uint32(tonopsTestTime.Unix()), emptyPreparedTestConfig()); err == nil {
		t.Fatal("nil outbound message should fail")
	}

	inbound, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("build inbound external message: %v", err)
	}
	if _, err = transactionOutboundMessageLayout(inbound); err == nil {
		t.Fatal("inbound external message should not have outbound layout")
	}
	if _, err = transactionNormalizeOutboundMessage(inbound, tonopsTestAddr, 1, uint32(tonopsTestTime.Unix()), emptyPreparedTestConfig()); err == nil {
		t.Fatal("normalizing inbound external message should fail")
	}

	malformed := cell.BeginCell().EndCell()
	if _, err = transactionOutboundMessageLayout(malformed); err == nil {
		t.Fatal("malformed outbound message should fail layout decoding")
	}
}

func FuzzTransactionOutboundMessageLayoutRoundTrip(f *testing.F) {
	f.Add(byte(0), true, true, true)
	f.Add(byte(1), true, false, false)
	f.Add(byte(2), false, true, true)
	f.Add(byte(3), false, false, false)

	f.Fuzz(func(t *testing.T, rawKind byte, hasState, stateInRef, bodyInRef bool) {
		body := cell.BeginCell().MustStoreUInt(uint64(rawKind), 8).EndCell()
		var state *tlb.StateInit
		if hasState {
			state = &tlb.StateInit{
				Data: cell.BeginCell().MustStoreUInt(uint64(rawKind)^0xff, 8).EndCell(),
			}
		}
		want := transactionOutboundLayout{
			stateInitInRef: hasState && stateInRef,
			bodyInRef:      bodyInRef,
		}
		layout := transactionOutboundLayout{stateInitInRef: stateInRef, bodyInRef: bodyInRef}

		var msgCell *cell.Cell
		var err error
		if rawKind&1 == 0 {
			msgCell, _, err = transactionInternalMessageToCellWithLayout(&tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     address.NewAddressNone(),
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(uint64(rawKind) + 1),
				StateInit:   state,
				Body:        body,
			}, layout)
		} else {
			msgCell, err = transactionExternalOutMessageToCell(&tlb.ExternalMessageOut{
				SrcAddr:   address.NewAddressNone(),
				DstAddr:   address.NewAddressExt(0, 8, []byte{rawKind}),
				StateInit: state,
				Body:      body,
			}, layout)
		}
		if err != nil {
			t.Fatalf("build outbound message: %v", err)
		}

		got, err := transactionOutboundMessageLayout(msgCell)
		if err != nil {
			t.Fatalf("decode outbound layout: %v", err)
		}
		if got != want {
			t.Fatalf("layout = %+v, want %+v", got, want)
		}
	})
}

func TestTransactionCollectUsageLoadsLazyRefs(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	child := cell.BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(leaf).EndCell()
	root := cell.BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(child).EndCell()

	lazyRoot := lazyTransactionTestRoot(t, root)
	lazyChild, err := lazyRoot.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !lazyChild.IsLazy() {
		t.Fatal("test root should keep child behind lazy boundary")
	}

	want, err := transactionCollectUsage(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := transactionCollectUsage(lazyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("usage from lazy tree = %+v, want %+v", got, want)
	}
}

func TestTransactionCollectUsageDeduplicatesSharedRefs(t *testing.T) {
	shared := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	root := cell.BeginCell().MustStoreUInt(0xA, 4).
		MustStoreRef(shared).
		MustStoreRef(shared).
		EndCell()

	got, err := transactionCollectUsage(root)
	if err != nil {
		t.Fatal(err)
	}

	want := transactionUsage{cells: 2, bits: 12}
	if got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
}

func TestTransactionMaxMerkleDepthNestedProofs(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	innerProof, err := cell.CreateMerkleProof(leaf)
	if err != nil {
		t.Fatal(err)
	}

	outerBody := cell.BeginCell().MustStoreRef(innerProof).EndCell()
	outerProof, err := cell.CreateMerkleProof(outerBody)
	if err != nil {
		t.Fatal(err)
	}

	got, err := transactionMaxMerkleDepth(outerProof)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("max merkle depth = %d, want 2", got)
	}
}

func TestTransactionValidateRelaxedActionMessageCurrencies(t *testing.T) {
	t.Run("nil message is invalid", func(t *testing.T) {
		_, err := transactionValidateRelaxedActionMessageCurrencies(nil)
		if !errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
			t.Fatalf("expected invalid relaxed message, got %v", err)
		}
	})

	t.Run("external out is accepted and external in is rejected", func(t *testing.T) {
		externalOut := cell.BeginCell().
			MustStoreBoolBit(true).
			MustStoreBoolBit(true).
			EndCell()
		if _, err := transactionValidateRelaxedActionMessageCurrencies(externalOut); err != nil {
			t.Fatalf("external out relaxed message should be accepted: %v", err)
		}

		externalIn := cell.BeginCell().
			MustStoreBoolBit(true).
			MustStoreBoolBit(false).
			EndCell()
		_, err := transactionValidateRelaxedActionMessageCurrencies(externalIn)
		if !errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
			t.Fatalf("expected external in relaxed message error, got %v", err)
		}
	})

	t.Run("reports non canonical value", func(t *testing.T) {
		msg := transactionTestRelaxedInternalMessage(func(b *cell.Builder) {
			b.MustStoreUInt(4, 4).
				MustStoreSlice([]byte{0x00, 0x0a, 0xb1, 0x47}, 32)
		}, func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(0))
		})

		res, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if err != nil {
			t.Fatalf("non-canonical value should keep message structurally valid: %v", err)
		}
		if res.valueCanonical {
			t.Fatal("expected non-canonical value to be reported")
		}
	})

	t.Run("reports non canonical extra flags as skippable", func(t *testing.T) {
		msg := transactionTestRelaxedInternalMessage(func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(700743))
		}, func(b *cell.Builder) {
			b.MustStoreUInt(1, 4).MustStoreUInt(0, 8)
		})

		res, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if err != nil {
			t.Fatalf("extra flags should not be fatal: %v", err)
		}
		if res.extraFlagsCanonical {
			t.Fatal("expected non-canonical extra flags to be reported")
		}
	})

	t.Run("reports truncated extra flags as invalid relaxed message", func(t *testing.T) {
		msg := transactionTestTruncatedRelaxedInternalMessage()

		_, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if !errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
			t.Fatalf("expected invalid relaxed message, got %v", err)
		}
	})

	t.Run("reports truncated extra currency marker as invalid relaxed message", func(t *testing.T) {
		msg := transactionTestTruncatedRelaxedInternalMessageExtraCurrencyMarker()

		_, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if !errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
			t.Fatalf("expected invalid relaxed message, got %v", err)
		}
	})

	t.Run("reports truncated fwd fee as invalid relaxed message", func(t *testing.T) {
		msg := transactionTestTruncatedRelaxedInternalMessageFwdFee()

		_, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if !errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
			t.Fatalf("expected invalid relaxed message, got %v", err)
		}
	})

	t.Run("reports extra currency trailing data", func(t *testing.T) {
		extra := cell.NewDict(32)
		if err := extra.SetIntKey(big.NewInt(7), cell.BeginCell().MustStoreBigVarUInt(big.NewInt(11), 32).MustStoreUInt(1, 1).EndCell()); err != nil {
			t.Fatal(err)
		}
		msg := transactionTestRelaxedInternalMessageWithExtra(extra, false)

		_, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if err == nil || err.Error() != "extra currency value has trailing data" {
			t.Fatalf("expected extra currency trailing data error, got %v", err)
		}
	})

	t.Run("reports trailing data after body ref", func(t *testing.T) {
		msg := transactionTestRelaxedInternalMessageWithExtra(nil, true)

		_, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if !errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
			t.Fatalf("expected trailing body data error, got %v", err)
		}
	})
}

func TestTransactionLoadActionsAndMalformedSendModeEdges(t *testing.T) {
	for _, root := range []*cell.Cell{nil, cell.BeginCell().EndCell()} {
		loaded, err := transactionLoadActions(root, 13)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.resultCode != 0 || len(loaded.actions) != 0 || loaded.totalActions != 0 {
			t.Fatalf("empty action list result = %+v", loaded)
		}
	}

	noPrevRef := cell.BeginCell().MustStoreUInt(0x0ec3c86d, 32).EndCell()
	loaded, err := transactionLoadActions(noPrevRef, 13)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.resultCode != 32 || loaded.resultArg != nil {
		t.Fatalf("no-prev-ref action result = %+v, want code 32 with nil arg", loaded)
	}

	tooMany := make([]any, 256)
	for i := range tooMany {
		tooMany[i] = tlb.ActionSetCode{NewCode: cell.BeginCell().MustStoreUInt(uint64(i&0xff), 8).EndCell()}
	}
	loaded, err = transactionLoadActions(buildTransactionActionList(t, tooMany...), 13)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.resultCode != 33 || loaded.resultArg == nil || *loaded.resultArg != 256 {
		t.Fatalf("too-many-actions result = %+v, want code 33 arg 256", loaded)
	}

	malformedBounce := cell.BeginCell().
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreUInt(0x0ec3c86d, 32).
		MustStoreUInt(16, 8).
		EndCell()
	v3, err := transactionLoadActions(malformedBounce, 3)
	if err != nil {
		t.Fatal(err)
	}
	if v3.resultCode != 34 || v3.bounce {
		t.Fatalf("v3 malformed send result = %+v, want code 34 without bounce", v3)
	}
	v4, err := transactionLoadActions(malformedBounce, 4)
	if err != nil {
		t.Fatal(err)
	}
	if v4.resultCode != 34 || !v4.bounce {
		t.Fatalf("v4 malformed send result = %+v, want code 34 with bounce", v4)
	}

	if mode, ok := transactionMalformedSendMode(malformedBounce); !ok || mode != 16 {
		t.Fatalf("malformed send mode = %d/%t, want 16/true", mode, ok)
	}
	for _, node := range []*cell.Cell{
		nil,
		cell.BeginCell().EndCell(),
		cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).MustStoreUInt(0xdeadbeef, 32).MustStoreUInt(2, 8).EndCell(),
		cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).MustStoreUInt(0x0ec3c86d, 31).EndCell(),
	} {
		if mode, ok := transactionMalformedSendMode(node); ok || mode != 0 {
			t.Fatalf("unexpected malformed send mode for %v: %d/%t", node, mode, ok)
		}
	}
}

func TestTransactionRelaxedVarUIntCanonicalEdges(t *testing.T) {
	if _, err := transactionLoadRelaxedVarUIntCanonical(cell.BeginCell().EndCell().MustBeginParse(), 0, false); !errors.Is(err, cell.ErrInvalidSize) {
		t.Fatalf("size 0 error = %v, want ErrInvalidSize", err)
	}
	if canonical, err := transactionLoadRelaxedVarUIntCanonical(cell.BeginCell().MustStoreUInt(0, 5).EndCell().MustBeginParse(), 32, true); err != nil || canonical {
		t.Fatalf("positive zero canonical = %t, %v; want false, nil", canonical, err)
	}
}

func TestTransactionValidateRelaxedActionMessageTailEdges(t *testing.T) {
	okTail := cell.BeginCell().
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 32)
	if err := transactionStoreStateInit(okTail, &tlb.StateInit{}, false); err != nil {
		t.Fatal(err)
	}
	if err := transactionStoreMessageBody(okTail, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := transactionValidateRelaxedActionMessageTail(okTail.EndCell().MustBeginParse()); err != nil {
		t.Fatalf("valid inline init/body tail failed: %v", err)
	}

	cases := []struct {
		name string
		tail *cell.Cell
	}{
		{name: "short created fields", tail: cell.BeginCell().MustStoreUInt(0, 64).EndCell()},
		{name: "missing init flag", tail: cell.BeginCell().MustStoreUInt(0, 64).MustStoreUInt(0, 32).EndCell()},
		{name: "missing init location", tail: cell.BeginCell().MustStoreUInt(0, 64).MustStoreUInt(0, 32).MustStoreBoolBit(true).EndCell()},
		{name: "missing init ref", tail: cell.BeginCell().MustStoreUInt(0, 64).MustStoreUInt(0, 32).MustStoreBoolBit(true).MustStoreBoolBit(true).EndCell()},
		{name: "malformed inline init", tail: cell.BeginCell().MustStoreUInt(0, 64).MustStoreUInt(0, 32).MustStoreBoolBit(true).MustStoreBoolBit(false).MustStoreBoolBit(true).EndCell()},
		{name: "missing body flag", tail: cell.BeginCell().MustStoreUInt(0, 64).MustStoreUInt(0, 32).MustStoreBoolBit(false).EndCell()},
		{name: "missing body ref", tail: cell.BeginCell().MustStoreUInt(0, 64).MustStoreUInt(0, 32).MustStoreBoolBit(false).MustStoreBoolBit(true).EndCell()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := transactionValidateRelaxedActionMessageTail(tc.tail.MustBeginParse())
			if !errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
				t.Fatalf("expected invalid relaxed tail, got %v", err)
			}
		})
	}
}

func TestTransactionApplyActionsInvalidRelaxedActionMessage(t *testing.T) {
	badInternalDst := buildTransactionOutboundInternalCellWithAddresses(
		t,
		address.NewAddressNone(),
		address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x33}, 32)),
		1000,
		cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	)
	extInMsg, err := tlb.ToCell(&tlb.ExternalMessage{
		SrcAddr: address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}),
		DstAddr: tonopsTestAddr,
		Body:    cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external inbound message: %v", err)
	}
	truncatedExtraCurrencyMarker := transactionTestTruncatedRelaxedInternalMessageExtraCurrencyMarker()
	truncatedExtraFlags := transactionTestTruncatedRelaxedInternalMessage()
	truncatedFwdFee := transactionTestTruncatedRelaxedInternalMessageFwdFee()

	for _, tc := range []struct {
		name        string
		mode        uint8
		msg         *cell.Cell
		wantSuccess bool
		wantValid   bool
		wantCode    int32
		wantSkipped uint16
	}{
		{
			name:        "internal with external destination aborts as invalid",
			mode:        64,
			msg:         badInternalDst,
			wantSuccess: false,
			wantValid:   false,
			wantCode:    34,
		},
		{
			name:        "internal with external destination is skipped with mode 2",
			mode:        2,
			msg:         badInternalDst,
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:        "external inbound action aborts as invalid",
			mode:        0,
			msg:         extInMsg,
			wantSuccess: false,
			wantValid:   false,
			wantCode:    34,
		},
		{
			name:        "external inbound action is skipped with mode 2",
			mode:        2,
			msg:         extInMsg,
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:        "internal with truncated extra currency marker aborts as invalid",
			mode:        1,
			msg:         truncatedExtraCurrencyMarker,
			wantSuccess: false,
			wantValid:   false,
			wantCode:    34,
		},
		{
			name:        "internal with truncated extra currency marker is skipped with mode 2",
			mode:        3,
			msg:         truncatedExtraCurrencyMarker,
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:        "internal with truncated extra flags aborts as invalid",
			mode:        1,
			msg:         truncatedExtraFlags,
			wantSuccess: false,
			wantValid:   false,
			wantCode:    34,
		},
		{
			name:        "internal with truncated extra flags is skipped with mode 2",
			mode:        3,
			msg:         truncatedExtraFlags,
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:        "internal with truncated fwd fee aborts as invalid",
			mode:        1,
			msg:         truncatedFwdFee,
			wantSuccess: false,
			wantValid:   false,
			wantCode:    34,
		},
		{
			name:        "internal with truncated fwd fee is skipped with mode 2",
			mode:        3,
			msg:         truncatedFwdFee,
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := applyTransactionSendActionForTest(t, tc.mode, tc.msg)
			if res.phase == nil {
				t.Fatal("missing action phase")
			}
			if res.phase.Success != tc.wantSuccess || res.phase.Valid != tc.wantValid || res.phase.ResultCode != tc.wantCode || res.phase.SkippedActions != tc.wantSkipped || res.phase.MessagesCreated != 0 {
				t.Fatalf("unexpected action phase: %+v", res.phase)
			}
		})
	}
}

func TestTransactionSendMode2SkipStartsAtV8(t *testing.T) {
	badInternalDst := buildTransactionOutboundInternalCellWithAddresses(
		t,
		address.NewAddressNone(),
		address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x33}, 32)),
		1000,
		cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	)

	for _, tc := range []struct {
		name        string
		version     uint32
		wantSuccess bool
		wantValid   bool
		wantCode    int32
		wantSkipped uint16
	}{
		{name: "v7", version: 7, wantCode: 34},
		{name: "v8", version: 8, wantSuccess: true, wantValid: true, wantSkipped: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := applyTransactionSendActionForTestWithParams(t,
				tlb.ActionSendMsg{Mode: 2, Msg: badInternalDst},
				transactionTestConfigWithGlobalVersion(t, tc.version),
				big.NewInt(1_000_000_000),
				nil,
				transactionZeroCurrencyBalance(),
			)
			if res.phase == nil {
				t.Fatal("missing action phase")
			}
			if res.phase.Success != tc.wantSuccess || res.phase.Valid != tc.wantValid || res.phase.ResultCode != tc.wantCode || res.phase.SkippedActions != tc.wantSkipped {
				t.Fatalf("unexpected action phase: %+v", res.phase)
			}
		})
	}
}

func TestTransactionMalformedSendPrepassSkipStartsAtV8(t *testing.T) {
	malformed := cell.BeginCell().
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreUInt(0x0ec3c86d, 32).
		MustStoreUInt(2, 8).
		EndCell()

	v7, err := transactionLoadActions(malformed, 7)
	if err != nil {
		t.Fatal(err)
	}
	if v7.resultCode != 34 || v7.skippedActions != 0 {
		t.Fatalf("unexpected v7 load result: %+v", v7)
	}

	v8, err := transactionLoadActions(malformed, 8)
	if err != nil {
		t.Fatal(err)
	}
	if v8.resultCode != 0 || v8.skippedActions != 1 || len(v8.actions) != 1 || !v8.actions[0].skipped {
		t.Fatalf("unexpected v8 load result: %+v", v8)
	}
}

func TestTransactionNonCanonicalExtraFlagsPrepassBeforeV8(t *testing.T) {
	msg := transactionTestRelaxedInternalMessageWithFwdFee(func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(1_000))
	}, func(b *cell.Builder) {
		b.MustStoreUInt(1, 4).MustStoreUInt(0, 8)
	}, func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(0))
	})

	for _, tc := range []struct {
		version     uint32
		wantSuccess bool
		wantCode    int32
		wantMsgs    uint16
	}{
		{version: 7, wantCode: 34},
		{version: 8, wantSuccess: true, wantMsgs: 1},
	} {
		t.Run("v"+big.NewInt(int64(tc.version)).String(), func(t *testing.T) {
			res := applyTransactionSendActionForTestWithParams(t,
				tlb.ActionSendMsg{Mode: 0, Msg: msg},
				transactionTestConfigWithGlobalVersion(t, tc.version),
				big.NewInt(1_000_000_000),
				nil,
				transactionZeroCurrencyBalance(),
			)
			if res.phase == nil {
				t.Fatal("missing action phase")
			}
			if res.phase.Success != tc.wantSuccess || res.phase.ResultCode != tc.wantCode || res.phase.MessagesCreated != tc.wantMsgs {
				t.Fatalf("unexpected action phase: %+v", res.phase)
			}
		})
	}
}

func TestTransactionInvalidSendModeSkipStartsAtV13(t *testing.T) {
	for _, tc := range []struct {
		name        string
		version     uint32
		wantSuccess bool
		wantCode    int32
		wantSkipped uint16
	}{
		{name: "v12", version: 12, wantCode: 34},
		{name: "v13", version: 13, wantSuccess: true, wantSkipped: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := applyTransactionSendActionForTestWithParams(t,
				tlb.ActionSendMsg{Mode: 6, Msg: buildTransactionOutboundInternalCell(t, 0)},
				transactionTestConfigWithGlobalVersion(t, tc.version),
				big.NewInt(1_000_000_000),
				nil,
				transactionZeroCurrencyBalance(),
			)
			if res.phase == nil {
				t.Fatal("missing action phase")
			}
			if res.phase.Success != tc.wantSuccess || res.phase.ResultCode != tc.wantCode || res.phase.SkippedActions != tc.wantSkipped {
				t.Fatalf("unexpected action phase: %+v", res.phase)
			}
		})
	}
}

func TestTransactionCustomForwardFeesDisabledFromV8(t *testing.T) {
	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: false,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(100),
		IHRFee:      tlb.FromNanoTONU(5),
		FwdFee:      tlb.FromNanoTONU(70),
		Body:        cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build outbound message: %v", err)
	}

	for _, tc := range []struct {
		name       string
		version    uint32
		wantAmount uint64
		wantFwdFee uint64
		wantIHRFee uint64
		wantTotal  uint64
	}{
		{name: "v7", version: 7, wantAmount: 25, wantFwdFee: 70, wantIHRFee: 5, wantTotal: 75},
		{name: "v8", version: 8, wantAmount: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := applyTransactionSendActionForTestWithParams(t,
				tlb.ActionSendMsg{Mode: 0, Msg: msgCell},
				transactionTestConfigWithGlobalVersion(t, tc.version),
				big.NewInt(1_000_000_000),
				nil,
				transactionZeroCurrencyBalance(),
			)
			if res.phase == nil || !res.phase.Success || len(res.outMsgs) != 1 {
				t.Fatalf("unexpected action result: phase=%+v out=%d", res.phase, len(res.outMsgs))
			}

			var outMsg tlb.Message
			if err := tlb.Parse(&outMsg, res.outMsgs[0].Cell); err != nil {
				t.Fatalf("failed to parse outbound message: %v", err)
			}
			internal := outMsg.AsInternal()
			if got := internal.Amount.Nano().Uint64(); got != tc.wantAmount {
				t.Fatalf("amount = %d, want %d", got, tc.wantAmount)
			}
			if got := internal.FwdFee.Nano().Uint64(); got != tc.wantFwdFee {
				t.Fatalf("fwd fee = %d, want %d", got, tc.wantFwdFee)
			}
			if got := internal.IHRFee.Nano().Uint64(); got != tc.wantIHRFee {
				t.Fatalf("ihr fee = %d, want %d", got, tc.wantIHRFee)
			}
			if got := transactionBigOrZero(transactionCoinsNano(res.phase.TotalFwdFees)).Uint64(); got != tc.wantTotal {
				t.Fatalf("total fwd fees = %d, want %d", got, tc.wantTotal)
			}
		})
	}
}

func TestTransactionNonCanonicalForwardFeeVersionGate(t *testing.T) {
	priceCell := buildTransactionMsgForwardPricesCell(t, 100, 1<<15)
	msgCell := transactionTestRelaxedInternalMessageWithFwdFee(func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(1))
	}, func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(0))
	}, func(b *cell.Builder) {
		b.MustStoreUInt(1, 4).
			MustStoreUInt(0, 8)
	})

	for _, tc := range []struct {
		name        string
		version     uint32
		mode        uint8
		wantSuccess bool
		wantCode    int32
		wantNoFunds bool
		wantSkipped uint16
	}{
		{name: "v7 aborts as invalid action", version: 7, wantCode: 34},
		{name: "v7 mode 2 still aborts", version: 7, mode: 2, wantCode: 34},
		{name: "v8 fails as ordinary send no funds", version: 8, wantCode: 37, wantNoFunds: true},
		{name: "v8 mode 2 skips ordinary send failure", version: 8, mode: 2, wantSuccess: true, wantSkipped: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, tc.version),
				tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
				tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
			})
			res := applyTransactionSendActionForTestWithParams(t,
				tlb.ActionSendMsg{Mode: tc.mode, Msg: msgCell},
				cfg,
				big.NewInt(1_000_000_000),
				nil,
				transactionZeroCurrencyBalance(),
			)
			if res.phase == nil {
				t.Fatal("missing action phase")
			}
			if res.phase.Success != tc.wantSuccess || res.phase.ResultCode != tc.wantCode || res.phase.NoFunds != tc.wantNoFunds || res.phase.SkippedActions != tc.wantSkipped || res.phase.MessagesCreated != 0 {
				t.Fatalf("unexpected action phase: %+v", res.phase)
			}
		})
	}
}

func TestTransactionExternalOutActionSuccessAndFees(t *testing.T) {
	priceCell := buildTransactionMsgForwardPricesCell(t, 100, 1<<15)
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 13),
		tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
		tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
	})
	balance := big.NewInt(1_000_000)
	msg := buildTransactionExternalOutCell(t, address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}))

	out := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{
		Mode: 0,
		Msg:  msg,
	}, cfg, balance, nil, transactionZeroCurrencyBalance())
	if out.phase == nil || !out.phase.Success || !out.phase.Valid || out.phase.ResultCode != 0 || out.phase.MessagesCreated != 1 || len(out.outMsgs) != 1 {
		t.Fatalf("unexpected action result: phase=%+v out=%d", out.phase, len(out.outMsgs))
	}

	fee := transactionBigOrZero(transactionCoinsNano(out.phase.TotalFwdFees))
	if fee.Sign() <= 0 {
		t.Fatalf("total forward fees = %s, want positive", fee)
	}
	if got := transactionBigOrZero(transactionCoinsNano(out.phase.TotalActionFees)); got.Cmp(fee) != 0 {
		t.Fatalf("total action fees = %s, want %s", got, fee)
	}
	if want := new(big.Int).Sub(balance, fee); out.balance.Cmp(want) != 0 {
		t.Fatalf("remaining balance = %s, want %s", out.balance, want)
	}

	var outMsg tlb.Message
	if err := transactionParseCell(&outMsg, out.outMsgs[0].Cell); err != nil {
		t.Fatalf("failed to parse external outbound message: %v", err)
	}
	ext := outMsg.AsExternalOut()
	if ext.SrcAddr == nil || !ext.SrcAddr.Equals(tonopsTestAddr) {
		t.Fatalf("src addr = %v, want %v", ext.SrcAddr, tonopsTestAddr)
	}
	if ext.CreatedLT != uint64(transactionTestLogicalTime+1) || ext.CreatedAt != uint32(tonopsTestTime.Unix()) {
		t.Fatalf("created fields = %d/%d, want %d/%d", ext.CreatedLT, ext.CreatedAt, transactionTestLogicalTime+1, uint32(tonopsTestTime.Unix()))
	}
}

func TestTransactionExternalOutActionInvalidDestinationAndMode(t *testing.T) {
	validDstMsg := buildTransactionExternalOutCell(t, address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}))
	stdDstMsg := buildTransactionExternalOutCell(t, tonopsTestAddr)

	for _, tc := range []struct {
		name        string
		mode        uint8
		msg         *cell.Cell
		wantSuccess bool
		wantValid   bool
		wantCode    int32
		wantSkipped uint16
	}{
		{
			name:        "std destination skipped with ignore errors",
			mode:        2,
			msg:         stdDstMsg,
			wantSuccess: true,
			wantValid:   true,
			wantSkipped: 1,
		},
		{
			name:      "mode 32 is invalid for external out",
			mode:      32,
			msg:       validDstMsg,
			wantValid: true,
			wantCode:  34,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{
				Mode: tc.mode,
				Msg:  tc.msg,
			}, transactionTestConfigWithGlobalVersion(t, 13), big.NewInt(1_000_000), nil, transactionZeroCurrencyBalance())
			if out.phase == nil {
				t.Fatal("missing action phase")
			}
			if out.phase.Success != tc.wantSuccess || out.phase.Valid != tc.wantValid || out.phase.ResultCode != tc.wantCode || out.phase.SkippedActions != tc.wantSkipped || out.phase.MessagesCreated != 0 {
				t.Fatalf("unexpected action phase: %+v", out.phase)
			}
		})
	}
}

func TestTransactionExternalOutActionNoFundsAndSpecialFees(t *testing.T) {
	priceCell := buildTransactionMsgForwardPricesCell(t, 100, 1<<15)
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 13),
		tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
		tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
	})
	msg := buildTransactionExternalOutCell(t, address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}))

	regular, err := transactionProcessSendAction(&transactionRuntimeAccount{addr: tonopsTestAddr}, tlb.ActionSendMsg{
		Mode: 0,
		Msg:  msg,
	}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), cfg, 13, &transactionCurrencyBalance{grams: big.NewInt(1)}, transactionZeroCurrencyBalance(), big.NewInt(0), big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	if regular.resultCode != 37 || regular.skipped || regular.msgCell != nil || regular.actionFine == nil {
		t.Fatalf("unexpected regular result: %+v", regular)
	}

	special, err := transactionProcessSendAction(&transactionRuntimeAccount{addr: tonopsTestAddr, isSpecial: true}, tlb.ActionSendMsg{
		Mode: 0,
		Msg:  msg,
	}, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), cfg, 13, transactionZeroCurrencyBalance(), transactionZeroCurrencyBalance(), big.NewInt(0), big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	if special.resultCode != 0 || special.msgCell == nil || special.debit.grams.Sign() != 0 || special.totalFwdFees.Sign() != 0 || special.totalActionFees.Sign() != 0 {
		t.Fatalf("unexpected special result: %+v", special)
	}
}

func TestTransactionSendActionPrepassMessageTypeEdges(t *testing.T) {
	invalidSourceExtOut, err := tlb.ToCell(&tlb.ExternalMessageOut{
		SrcAddr: address.NewAddress(0, byte(tonopsTestAddr.Workchain()), bytes.Repeat([]byte{0x33}, 32)),
		DstAddr: address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}),
		Body:    cell.BeginCell().MustStoreUInt(0xE0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external outbound message: %v", err)
	}
	externalInAsAction, err := tlb.ToCell(&tlb.ExternalMessage{
		SrcAddr: address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}),
		DstAddr: tonopsTestAddr,
		Body:    cell.BeginCell().MustStoreUInt(0xE0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external inbound message: %v", err)
	}
	internalExtDest, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}),
		Amount:      tlb.FromNanoTONU(1),
		Body:        cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build internal message with external destination: %v", err)
	}

	for _, tc := range []struct {
		name        string
		msg         *cell.Cell
		wantCode    int32
		wantInvalid bool
	}{
		{name: "external out invalid source", msg: invalidSourceExtOut, wantCode: 35},
		{name: "external in is not outbound action", msg: externalInAsAction, wantCode: 34, wantInvalid: true},
		{name: "internal external destination type", msg: internalExtDest, wantCode: 34, wantInvalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := transactionProcessSendAction(
				&transactionRuntimeAccount{addr: tonopsTestAddr},
				tlb.ActionSendMsg{Mode: 0, Msg: tc.msg},
				uint64(transactionTestLogicalTime),
				uint32(tonopsTestTime.Unix()),
				transactionTestConfigWithGlobalVersion(t, 13),
				13,
				&transactionCurrencyBalance{grams: big.NewInt(1_000_000_000)},
				transactionZeroCurrencyBalance(),
				big.NewInt(0),
				big.NewInt(0),
			)
			if err != nil {
				t.Fatal(err)
			}
			if out.resultCode != tc.wantCode || out.invalid != tc.wantInvalid || out.skipped || out.msgCell != nil {
				t.Fatalf("unexpected send result: %+v", out)
			}
		})
	}
}

func TestTransactionSendActionSizeExtraAndFineFundsEdges(t *testing.T) {
	priceCell := buildTransactionMsgForwardPricesCell(t, 100, 1<<15)
	tinySizeCfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 13),
		tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
		tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
		tlb.ConfigParamSizeLimits:                  buildTransactionSizeLimitsCell(t, 1<<21, 1, 1000, 3, 2),
	})
	largeExtOut, err := tlb.ToCell(&tlb.ExternalMessageOut{
		SrcAddr: address.NewAddressNone(),
		DstAddr: address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}),
		Body:    transactionTestCellChain(20),
	})
	if err != nil {
		t.Fatalf("failed to build large external outbound message: %v", err)
	}
	extOut, err := transactionProcessSendAction(
		&transactionRuntimeAccount{addr: tonopsTestAddr},
		tlb.ActionSendMsg{Mode: 0, Msg: largeExtOut},
		uint64(transactionTestLogicalTime),
		uint32(tonopsTestTime.Unix()),
		tinySizeCfg,
		13,
		&transactionCurrencyBalance{grams: big.NewInt(1_000_000_000)},
		transactionZeroCurrencyBalance(),
		big.NewInt(0),
		big.NewInt(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if extOut.resultCode != 40 || extOut.actionFine == nil || extOut.msgCell != nil {
		t.Fatalf("unexpected external size result: %+v", extOut)
	}

	extra := cell.NewDict(32)
	for id := uint32(1); id <= 3; id++ {
		if err := extra.SetIntKey(new(big.Int).SetUint64(uint64(id)), cell.BeginCell().MustStoreBigVarUInt(new(big.Int).SetUint64(uint64(id)), 32).EndCell()); err != nil {
			t.Fatalf("failed to build extra currencies: %v", err)
		}
	}
	tooManyExtra, err := transactionProcessSendAction(
		&transactionRuntimeAccount{addr: tonopsTestAddr},
		tlb.ActionSendMsg{Mode: 0, Msg: buildTransactionOutboundInternalCellWithExtra(t, 0, extra)},
		uint64(transactionTestLogicalTime),
		uint32(tonopsTestTime.Unix()),
		transactionTestConfigWithGlobalVersion(t, 13),
		13,
		&transactionCurrencyBalance{grams: big.NewInt(1_000_000_000)},
		transactionZeroCurrencyBalance(),
		big.NewInt(0),
		big.NewInt(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if tooManyExtra.resultCode != 44 || tooManyExtra.skipped || tooManyExtra.msgCell != nil {
		t.Fatalf("unexpected extra-currency result: %+v", tooManyExtra)
	}

	mode64NoFunds, err := transactionProcessSendAction(
		&transactionRuntimeAccount{addr: tonopsTestAddr},
		tlb.ActionSendMsg{Mode: 64, Msg: buildTransactionOutboundInternalCell(t, 0)},
		uint64(transactionTestLogicalTime),
		uint32(tonopsTestTime.Unix()),
		transactionTestConfigWithGlobalVersion(t, 13),
		13,
		&transactionCurrencyBalance{grams: big.NewInt(1_000_000_000)},
		transactionZeroCurrencyBalance(),
		big.NewInt(2),
		big.NewInt(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if mode64NoFunds.resultCode != 37 || mode64NoFunds.skipped || mode64NoFunds.msgCell != nil {
		t.Fatalf("unexpected mode64 fine-funds result: %+v", mode64NoFunds)
	}
}

func TestTransactionReserveExtraVersionGates(t *testing.T) {
	action := tlb.ActionReserveCurrency{
		Mode: 2,
		Currency: tlb.CurrencyCollection{
			Coins:           tlb.FromNanoTONU(0),
			ExtraCurrencies: makeTransactionExtraCurrencies(t, 7, 11),
		},
	}

	for _, tc := range []struct {
		name              string
		version           uint32
		wantCode          int32
		wantReservedExtra uint64
	}{
		{name: "v8 does not clamp extra", version: 8, wantCode: 38},
		{name: "v9 clamps extra", version: 9, wantReservedExtra: 10},
		{name: "v10 rejects extra", version: 10, wantCode: 34},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := makeTransactionExtraCurrencies(t, 7, 10)
			original, err := transactionCurrencyFromParts(big.NewInt(100), extra)
			if err != nil {
				t.Fatal(err)
			}
			remaining := original.copy()
			reserved := transactionZeroCurrencyBalance()

			res, err := transactionProcessReserveAction(action, original, remaining, reserved, tc.version)
			if err != nil {
				t.Fatal(err)
			}
			if res.resultCode != tc.wantCode {
				t.Fatalf("result code = %d, want %d", res.resultCode, tc.wantCode)
			}
			if tc.wantReservedExtra > 0 {
				got := reserved.extra[7]
				if got == nil || got.Uint64() != tc.wantReservedExtra {
					t.Fatalf("reserved extra = %v, want %d", got, tc.wantReservedExtra)
				}
			}
		})
	}
}

func TestTransactionReserveActionModeAndBalanceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		version      uint32
		mode         uint8
		amount       uint64
		remaining    uint64
		wantCode     int32
		wantRemain   uint64
		wantReserved uint64
		wantBounce   bool
	}{
		{name: "invalid high mode", version: 13, mode: 32, amount: 1, remaining: 100, wantCode: 34, wantRemain: 100},
		{name: "mode 8 without original balance mode", version: 13, mode: 8, amount: 1, remaining: 100, wantCode: 34, wantRemain: 100},
		{name: "v8 mode 2 clamps grams", version: 8, mode: 2, amount: 150, remaining: 100, wantRemain: 0, wantReserved: 100},
		{name: "mode 0 insufficient grams", version: 13, mode: 0, amount: 101, remaining: 100, wantCode: 37, wantRemain: 100},
		{name: "mode 1 swaps remaining before v10", version: 9, mode: 1, amount: 40, remaining: 100, wantRemain: 40, wantReserved: 60},
		{name: "mode 1 swaps grams from v10", version: 10, mode: 1, amount: 40, remaining: 100, wantRemain: 40, wantReserved: 60},
		{name: "mode 16 bounces insufficient grams from v4", version: 4, mode: 16, amount: 101, remaining: 100, wantCode: 37, wantRemain: 100, wantBounce: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original, err := transactionCurrencyFromParts(new(big.Int).SetUint64(tc.remaining), nil)
			if err != nil {
				t.Fatal(err)
			}
			remaining := original.copy()
			reserved := transactionZeroCurrencyBalance()

			res, err := transactionProcessReserveAction(tlb.ActionReserveCurrency{
				Mode: tc.mode,
				Currency: tlb.CurrencyCollection{
					Coins: tlb.FromNanoTONU(tc.amount),
				},
			}, original, remaining, reserved, tc.version)
			if err != nil {
				t.Fatal(err)
			}
			if res.resultCode != tc.wantCode || res.bounceOnFail != tc.wantBounce {
				t.Fatalf("reserve result = %+v, want code %d bounce %t", res, tc.wantCode, tc.wantBounce)
			}
			if got := remaining.grams.Uint64(); got != tc.wantRemain {
				t.Fatalf("remaining grams = %d, want %d", got, tc.wantRemain)
			}
			if got := reserved.grams.Uint64(); got != tc.wantReserved {
				t.Fatalf("reserved grams = %d, want %d", got, tc.wantReserved)
			}
		})
	}
}

func TestTransactionReserveOriginalMinusAmountFailures(t *testing.T) {
	for _, version := range []uint32{9, 10} {
		t.Run("v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			original, err := transactionCurrencyFromParts(big.NewInt(50), nil)
			if err != nil {
				t.Fatal(err)
			}
			remaining := original.copy()
			reserved := transactionZeroCurrencyBalance()

			res, err := transactionProcessReserveAction(tlb.ActionReserveCurrency{
				Mode: 12,
				Currency: tlb.CurrencyCollection{
					Coins: tlb.FromNanoTONU(51),
				},
			}, original, remaining, reserved, version)
			if err != nil {
				t.Fatal(err)
			}
			if res.resultCode != 34 {
				t.Fatalf("result code = %d, want 34", res.resultCode)
			}
			if remaining.grams.Uint64() != 50 || reserved.grams.Sign() != 0 {
				t.Fatalf("remaining/reserved = %s/%s, want unchanged/zero", remaining.grams, reserved.grams)
			}
		})
	}
}

func TestTransactionReserveActionExtraFailureKeepsBalances(t *testing.T) {
	original, err := transactionCurrencyFromParts(big.NewInt(100), makeTransactionExtraCurrencies(t, 7, 5))
	if err != nil {
		t.Fatal(err)
	}
	remaining := original.copy()
	reserved := transactionZeroCurrencyBalance()

	res, err := transactionProcessReserveAction(tlb.ActionReserveCurrency{
		Mode: 0,
		Currency: tlb.CurrencyCollection{
			Coins:           tlb.FromNanoTONU(1),
			ExtraCurrencies: makeTransactionExtraCurrencies(t, 7, 6),
		},
	}, original, remaining, reserved, 9)
	if err != nil {
		t.Fatal(err)
	}
	if res.resultCode != 38 {
		t.Fatalf("result code = %d, want 38", res.resultCode)
	}
	if remaining.grams.Uint64() != 100 || transactionExtraCount(remaining.extra) != 1 || reserved.grams.Sign() != 0 || !reserved.extraEmpty() {
		t.Fatalf("balances changed after failed reserve: remaining=%+v reserved=%+v", remaining, reserved)
	}
}

func TestTransactionApplyActionsV13InvalidActionFormsAffectValidityLikeCpp(t *testing.T) {
	truncatedNonCanonicalValue := cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(tonopsTestAddr).
		MustStoreUInt(1, 4).
		MustStoreUInt(0, 8).
		EndCell()
	extOutMsg, err := tlb.ToCell(&tlb.ExternalMessageOut{
		SrcAddr: address.NewAddressNone(),
		DstAddr: address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x11}, 32)),
		Body:    cell.BeginCell().MustStoreUInt(0xE0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external outbound message: %v", err)
	}

	for _, tc := range []struct {
		name        string
		actions     []any
		wantSuccess bool
		wantValid   bool
		wantCode    int32
		wantSkipped uint16
	}{
		{
			name:      "send invalid mode aborts as action error",
			actions:   []any{tlb.ActionSendMsg{Mode: 4, Msg: buildTransactionOutboundInternalCell(t, 0)}},
			wantCode:  34,
			wantValid: true,
		},
		{
			name:        "send invalid mode is skipped with ignore errors",
			actions:     []any{tlb.ActionSendMsg{Mode: 6, Msg: buildTransactionOutboundInternalCell(t, 0)}},
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:      "truncated non canonical value aborts as prepass invalid without ignore errors",
			actions:   []any{tlb.ActionSendMsg{Mode: 0, Msg: truncatedNonCanonicalValue}},
			wantCode:  34,
			wantValid: false,
		},
		{
			name:        "truncated non canonical value is skipped with ignore errors",
			actions:     []any{tlb.ActionSendMsg{Mode: 2, Msg: truncatedNonCanonicalValue}},
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:      "external outbound invalid mode is not skipped",
			actions:   []any{tlb.ActionSendMsg{Mode: 66, Msg: extOutMsg}},
			wantCode:  34,
			wantValid: true,
		},
		{
			name: "reserve extra currencies aborts as action error",
			actions: []any{tlb.ActionReserveCurrency{
				Mode: 0,
				Currency: tlb.CurrencyCollection{
					Coins:           tlb.FromNanoTONU(0),
					ExtraCurrencies: makeTransactionExtraCurrencies(t, 7, 11),
				},
			}},
			wantCode:  34,
			wantValid: true,
		},
		{
			name: "reserve original balance minus too large amount is regular failure",
			actions: []any{tlb.ActionReserveCurrency{
				Mode: 12,
				Currency: tlb.CurrencyCollection{
					Coins: tlb.FromNanoTONU(2_000_000_000),
				},
			}},
			wantCode:  34,
			wantValid: true,
		},
		{
			name: "change library invalid mode aborts as action error",
			actions: []any{tlb.ActionChangeLibrary{
				Mode:   3,
				LibRef: tlb.LibRefHash{LibHash: make([]byte, 32)},
			}},
			wantCode:  34,
			wantValid: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := applyTransactionActionsForTestWithParams(t, tc.actions, transactionTestConfigWithGlobalVersion(t, 13), big.NewInt(1_000_000_000), nil, transactionZeroCurrencyBalance())
			if out.phase == nil {
				t.Fatal("missing action phase")
			}
			if out.phase.Success != tc.wantSuccess || out.phase.Valid != tc.wantValid || out.phase.ResultCode != tc.wantCode || out.phase.SkippedActions != tc.wantSkipped {
				t.Fatalf("unexpected action phase: %+v", out.phase)
			}
		})
	}
}

func TestTransactionApplyActionsMode16StartsAtV4(t *testing.T) {
	action := tlb.ActionReserveCurrency{
		Mode: 16,
		Currency: tlb.CurrencyCollection{
			Coins: tlb.FromNanoTONU(2_000),
		},
	}

	v3 := applyTransactionActionsForTestWithParams(t, []any{action}, transactionTestConfigWithGlobalVersion(t, 3), big.NewInt(1_000), nil, transactionZeroCurrencyBalance())
	if v3.phase == nil || v3.phase.ResultCode != 34 || v3.phase.NoFunds || v3.bounce {
		t.Fatalf("unexpected v3 action phase: phase=%+v bounce=%t", v3.phase, v3.bounce)
	}

	v4 := applyTransactionActionsForTestWithParams(t, []any{action}, transactionTestConfigWithGlobalVersion(t, 4), big.NewInt(1_000), nil, transactionZeroCurrencyBalance())
	if v4.phase == nil || v4.phase.ResultCode != 37 || !v4.phase.NoFunds || !v4.bounce {
		t.Fatalf("unexpected v4 action phase: phase=%+v bounce=%t", v4.phase, v4.bounce)
	}
}

func TestTransactionApplyActionsV13MessageBalanceExtraSurvivesMode64(t *testing.T) {
	msgBalance := &transactionCurrencyBalance{
		grams: big.NewInt(500),
		extra: map[uint32]*big.Int{
			7: big.NewInt(11),
		},
	}
	out := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{
		Mode: 64,
		Msg:  buildTransactionOutboundInternalCell(t, 0),
	}, transactionTestConfigWithGlobalVersion(t, 13), big.NewInt(1_000), nil, msgBalance)

	if out.phase == nil || !out.phase.Success || out.phase.MessagesCreated != 1 {
		t.Fatalf("unexpected action phase: %+v", out.phase)
	}
	if out.msgBalanceRemaining.grams.Sign() != 0 {
		t.Fatalf("message balance grams = %s, want 0", out.msgBalanceRemaining.grams)
	}
	if got := out.msgBalanceRemaining.extra[7]; got == nil || got.Int64() != 11 {
		t.Fatalf("message balance extra = %v, want 11", out.msgBalanceRemaining.extra)
	}
}

func TestTransactionApplyActionsV13DeleteRequestIgnoresRemainingExtraCurrencies(t *testing.T) {
	extra := makeTransactionExtraCurrencies(t, 7, 11)
	out := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{
		Mode: 0xA0,
		Msg:  buildTransactionOutboundInternalCell(t, 0),
	}, transactionTestConfigWithGlobalVersion(t, 13), big.NewInt(1_000), extra, transactionZeroCurrencyBalance())

	if out.phase == nil || !out.phase.Success || out.phase.StatusChange.Type != tlb.AccStatusChangeDeleted {
		t.Fatalf("unexpected action phase: %+v", out.phase)
	}
	if !out.deleteAccount {
		t.Fatal("delete account should be requested when grams are exhausted even if extra currencies remain")
	}
	if out.balance.Sign() != 0 {
		t.Fatalf("remaining grams = %s, want 0", out.balance)
	}
	if transactionExtraDictIsEmpty(out.extraCurrencies) {
		t.Fatal("remaining extra currencies should stay on the account")
	}
}

func TestTransactionProcessReserveCurrencyV13DoesNotReserveOriginalExtraCurrencies(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mode        uint8
		amount      uint64
		wantRemain  uint64
		wantReserve uint64
	}{
		{name: "plus original balance", mode: 4, amount: 0, wantRemain: 0, wantReserve: 100},
		{name: "original balance minus amount", mode: 12, amount: 40, wantRemain: 40, wantReserve: 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := &transactionCurrencyBalance{
				grams: big.NewInt(100),
				extra: map[uint32]*big.Int{
					7: big.NewInt(11),
				},
			}
			remaining := original.copy()
			reserved := transactionZeroCurrencyBalance()

			res, err := transactionProcessReserveAction(tlb.ActionReserveCurrency{
				Mode: tc.mode,
				Currency: tlb.CurrencyCollection{
					Coins: tlb.FromNanoTONU(tc.amount),
				},
			}, original, remaining, reserved, vmcore.MaxSupportedGlobalVersion)
			if err != nil {
				t.Fatal(err)
			}
			if res.resultCode != 0 {
				t.Fatalf("reserve failed with code %d", res.resultCode)
			}
			if remaining.grams.Uint64() != tc.wantRemain || reserved.grams.Uint64() != tc.wantReserve {
				t.Fatalf("grams remaining/reserved = %s/%s, want %d/%d", remaining.grams, reserved.grams, tc.wantRemain, tc.wantReserve)
			}
			if got := remaining.extra[7]; got == nil || got.Int64() != 11 {
				t.Fatalf("remaining extra = %v, want currency 7 preserved", remaining.extra)
			}
			if !reserved.extraEmpty() {
				t.Fatalf("reserved extra = %v, want empty", reserved.extra)
			}
		})
	}
}

func TestTransactionApplyActionsActionFineStartsAtV4(t *testing.T) {
	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{CellPrice: 4 << 16})
	if err != nil {
		t.Fatalf("failed to build msg prices: %v", err)
	}
	body := transactionTestCellChain(20)

	for _, tc := range []struct {
		name        string
		version     uint32
		wantCode    int32
		wantFine    int64
		wantBalance int64
	}{
		{name: "v3 no fine", version: 3, wantCode: 37, wantBalance: 10},
		{name: "v4 fine", version: 4, wantCode: 40, wantFine: 10, wantBalance: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, tc.version),
				tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
				tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
			})
			out := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{
				Mode: 0,
				Msg:  buildTransactionOutboundInternalCellWithBody(t, 10, body),
			}, cfg, big.NewInt(10), nil, transactionZeroCurrencyBalance())

			if out.phase == nil || out.phase.Success || out.phase.ResultCode != tc.wantCode {
				t.Fatalf("unexpected action phase: %+v", out.phase)
			}
			gotFine := int64(0)
			if out.phase.TotalActionFees != nil {
				gotFine = out.phase.TotalActionFees.Nano().Int64()
			}
			if gotFine != tc.wantFine {
				t.Fatalf("action fine = %d, want %d", gotFine, tc.wantFine)
			}
			if out.balance.Int64() != tc.wantBalance {
				t.Fatalf("remaining balance = %s, want %d", out.balance, tc.wantBalance)
			}
		})
	}
}

func TestTransactionApplyActionsV15ChargesSuccessfulMessageFineOnLaterFailure(t *testing.T) {
	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{CellPrice: 4 << 16})
	if err != nil {
		t.Fatalf("failed to build msg prices: %v", err)
	}
	body := transactionTestCellChain(3)

	for _, tt := range []struct {
		name     string
		version  uint32
		wantFine bool
	}{
		{name: "v14 no successful message fine on later failure", version: 14},
		{name: "v15 charges successful message fine on later failure", version: 15, wantFine: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, tt.version),
				tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
				tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
			})
			out := applyTransactionActionsForTestWithParams(t, []any{
				tlb.ActionSendMsg{
					Mode: 1,
					Msg:  buildTransactionOutboundInternalCellWithBody(t, 1_000, body),
				},
				tlb.ActionChangeLibrary{
					Mode:   3,
					LibRef: tlb.LibRefHash{LibHash: make([]byte, 32)},
				},
			}, cfg, big.NewInt(1_000_000), nil, transactionZeroCurrencyBalance())

			if out.phase == nil || out.phase.Success || out.phase.ResultCode != 34 || out.phase.MessagesCreated != 1 {
				t.Fatalf("unexpected action phase: %+v", out.phase)
			}
			gotFine := out.actionFine.Sign() > 0
			if gotFine != tt.wantFine {
				t.Fatalf("charged action fine = %s, want positive=%t", out.actionFine, tt.wantFine)
			}
		})
	}
}

func TestTransactionV15ChangeLibraryRestrictions(t *testing.T) {
	lib := cell.BeginCell().MustStoreUInt(0xA6, 8).EndCell()
	cfg := transactionTestConfigWithGlobalVersion(t, 15)

	for _, tt := range []struct {
		name      string
		action    tlb.ActionChangeLibrary
		current   *cell.Dictionary
		isSpecial bool
		wantCode  int32
		wantStore bool
	}{
		{
			name:     "non-special cannot remove library",
			action:   tlb.ActionChangeLibrary{Mode: 0, LibRef: tlb.LibRefHash{LibHash: lib.Hash()}},
			current:  buildTransactionV13LibraryDict(t, lib, true),
			wantCode: 46,
		},
		{
			name:     "non-special cannot add public library",
			action:   tlb.ActionChangeLibrary{Mode: 2, LibRef: tlb.LibRefRef{Library: lib}},
			wantCode: 46,
		},
		{
			name:      "special cannot add private library",
			action:    tlb.ActionChangeLibrary{Mode: 1, LibRef: tlb.LibRefRef{Library: lib}},
			isSpecial: true,
			wantCode:  46,
		},
		{
			name:      "special can add public library",
			action:    tlb.ActionChangeLibrary{Mode: 2, LibRef: tlb.LibRefRef{Library: lib}},
			isSpecial: true,
			wantStore: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := transactionProcessChangeLibraryAction(tt.action, tt.current, cfg, 15, tt.isSpecial)
			if err != nil {
				t.Fatal(err)
			}
			if res.resultCode != tt.wantCode {
				t.Fatalf("result code = %d, want %d", res.resultCode, tt.wantCode)
			}
			if !tt.wantStore {
				if res.nextLibraries != nil {
					t.Fatalf("next libraries present on failure: %v", res.nextLibraries)
				}
				return
			}
			assertTransactionLibraryStored(t, res.nextLibraries, lib, true)
		})
	}
}

func TestTransactionFailedActionKeepsPreviousActionFeesBeforeV4(t *testing.T) {
	priceCell := buildTransactionMsgForwardPricesCell(t, 400000, 21845)
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 0),
		tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
		tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
	})
	msgBalance, err := transactionCurrencyFromParts(big.NewInt(1_000_000_000), nil)
	if err != nil {
		t.Fatal(err)
	}

	out := applyTransactionActionsForTestWithParams(t, []any{
		tlb.ActionSendMsg{Mode: 64, Msg: buildTransactionOutboundInternalCell(t, 1)},
		tlb.ActionSendMsg{Mode: 16, Msg: buildTransactionOutboundInternalCell(t, 20_000_000_000)},
	}, cfg, big.NewInt(2_000_000_000), nil, msgBalance)

	if out.phase == nil || out.phase.Success || out.phase.ResultCode != 34 {
		t.Fatalf("unexpected action phase: %+v", out.phase)
	}
	if out.phase.TotalActionFees == nil || out.phase.TotalActionFees.Nano().Sign() == 0 {
		t.Fatalf("total action fees = %v, want previous outbound action fees", out.phase.TotalActionFees)
	}
	if out.actionFees == nil || out.actionFees.Sign() != 0 {
		t.Fatalf("charged action fees = %v, want no previous outbound action fees charged", out.actionFees)
	}
}

func assertTransactionLibraryStored(t *testing.T, libs *cell.Dictionary, lib *cell.Cell, wantPublic bool) {
	t.Helper()

	if libs == nil {
		t.Fatal("next libraries are nil")
	}
	hash := lib.HashKey()
	key := cell.BeginCell().MustStoreSlice(hash[:], 256).EndCell()
	value, err := libs.LoadValue(key)
	if err != nil {
		t.Fatalf("failed to load library value: %v", err)
	}
	if value == nil {
		t.Fatal("library value is missing")
	}
	isPublic, err := value.LoadBoolBit()
	if err != nil {
		t.Fatalf("failed to load library public flag: %v", err)
	}
	if isPublic != wantPublic {
		t.Fatalf("library public flag = %t, want %t", isPublic, wantPublic)
	}
	ref, err := value.LoadRefCell()
	if err != nil {
		t.Fatalf("failed to load library ref: %v", err)
	}
	if ref == nil || ref.HashKey() != hash {
		t.Fatal("library ref hash mismatch")
	}
}

func applyTransactionSendActionForTest(t *testing.T, mode uint8, msg *cell.Cell) *transactionActionApplyResult {
	t.Helper()

	return applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{Mode: mode, Msg: msg}, transactionTestConfigWithGlobalVersion(t, 13), big.NewInt(1_000_000_000), nil, transactionZeroCurrencyBalance())
}

func applyTransactionSendActionForTestWithParams(t *testing.T, act tlb.ActionSendMsg, cfg *PreparedBlockchainConfig, balance *big.Int, extra *cell.Dictionary, msgBalance *transactionCurrencyBalance) *transactionActionApplyResult {
	t.Helper()

	return applyTransactionActionsForTestWithParams(t, []any{act}, cfg, balance, extra, msgBalance)
}

func buildTransactionExternalOutCell(t *testing.T, dst *address.Address) *cell.Cell {
	t.Helper()

	msg, err := tlb.ToCell(&tlb.ExternalMessageOut{
		SrcAddr: address.NewAddressNone(),
		DstAddr: dst,
		Body:    cell.BeginCell().MustStoreUInt(0xE0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external outbound message: %v", err)
	}
	return msg
}

func applyTransactionActionsForTestWithParams(t *testing.T, actions []any, cfg *PreparedBlockchainConfig, balance *big.Int, extra *cell.Dictionary, msgBalance *transactionCurrencyBalance) *transactionActionApplyResult {
	t.Helper()

	data := cell.BeginCell().EndCell()
	res := &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      data,
			Actions:   buildTransactionActionList(t, actions...),
			Committed: true,
		},
	}
	acc := &transactionRuntimeAccount{
		addr:    tonopsTestAddr,
		status:  tlb.AccountStatusActive,
		code:    cell.BeginCell().EndCell(),
		data:    data,
		balance: new(big.Int).Set(balance),
	}
	out, err := transactionApplyActions(acc, res, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), cfg, new(big.Int).Set(balance), extra, msgBalance, big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func transactionTestConfigWithGlobalVersion(t *testing.T, version uint32) *PreparedBlockchainConfig {
	t.Helper()

	return transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, version),
	})
}

func transactionTestConfigWithParams(t *testing.T, params map[uint32]*cell.Cell) *PreparedBlockchainConfig {
	t.Helper()

	if _, ok := params[tlb.ConfigParamGlobalVersion]; !ok {
		params[tlb.ConfigParamGlobalVersion] = transactionTestGlobalVersionCell(t, 13)
	}
	return MustPrepareBlockchainConfig(buildTransactionConfigRoot(t, params))
}

func transactionTestGlobalVersionCell(t *testing.T, version uint32) *cell.Cell {
	t.Helper()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: version})
	if err != nil {
		t.Fatalf("failed to build global version cell: %v", err)
	}
	return versionCell
}

func transactionTestCellChain(cells int) *cell.Cell {
	var next *cell.Cell
	for i := 0; i < cells; i++ {
		builder := cell.BeginCell().MustStoreUInt(uint64(i&0xff), 8)
		if next != nil {
			builder.MustStoreRef(next)
		}
		next = builder.EndCell()
	}
	return next
}

func transactionTestRelaxedInternalMessage(storeValue, storeExtraFlags func(*cell.Builder)) *cell.Cell {
	return transactionTestRelaxedInternalMessageWithFwdFee(storeValue, storeExtraFlags, func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(0))
	})
}

func transactionTestRelaxedInternalMessageWithFwdFee(storeValue, storeExtraFlags, storeFwdFee func(*cell.Builder)) *cell.Cell {
	return cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddress(0, 0, make([]byte, 32))).
		MustStoreBuilder(transactionTestBuilder(storeValue)).
		MustStoreBoolBit(false).
		MustStoreBuilder(transactionTestBuilder(storeExtraFlags)).
		MustStoreBuilder(transactionTestBuilder(storeFwdFee)).
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 32).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		EndCell()
}

func transactionTestRelaxedInternalMessageWithExtra(extra *cell.Dictionary, trailingAfterBody bool) *cell.Cell {
	builder := cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddress(0, 0, make([]byte, 32))).
		MustStoreBigCoins(big.NewInt(1)).
		MustStoreDict(extra).
		MustStoreBigCoins(big.NewInt(0)).
		MustStoreBigCoins(big.NewInt(0)).
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 32).
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreRef(cell.BeginCell().EndCell())
	if trailingAfterBody {
		builder.MustStoreBoolBit(true)
	}
	return builder.EndCell()
}

func transactionTestTruncatedRelaxedInternalMessage() *cell.Cell {
	return cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddress(0, 0, make([]byte, 32))).
		MustStoreBigCoins(big.NewInt(1)).
		MustStoreBoolBit(false).
		MustStoreUInt(1, 2).
		EndCell()
}

func transactionTestTruncatedRelaxedInternalMessageExtraCurrencyMarker() *cell.Cell {
	return cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddress(0, 0, make([]byte, 32))).
		MustStoreBigCoins(big.NewInt(1)).
		EndCell()
}

func transactionTestTruncatedRelaxedInternalMessageFwdFee() *cell.Cell {
	return cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddress(0, 0, make([]byte, 32))).
		MustStoreBigCoins(big.NewInt(1)).
		MustStoreBoolBit(false).
		MustStoreBigCoins(big.NewInt(0)).
		MustStoreUInt(1, 2).
		EndCell()
}

func transactionTestBuilder(fn func(*cell.Builder)) *cell.Builder {
	b := cell.BeginCell()
	fn(b)
	return b
}

func lazyTransactionTestRoot(t *testing.T, root *cell.Cell) *cell.Cell {
	t.Helper()

	roots, _, err := cell.FromBOCMultiRootReader(
		cell.NewBOCNoCopyReader(root.ToBOCWithOptions(cell.BOCSerializeOptions{WithIndex: true})),
		cell.BOCParseOptions{Lazy: true, TrustedHashes: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("lazy parse returned %d roots, want 1", len(roots))
	}
	return roots[0]
}
