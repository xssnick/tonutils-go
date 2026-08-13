package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// buildLayoutMessageCell writes an internal message by hand so the Either
// choices are pinned instead of being decided by what fits.
func buildLayoutMessageCell(t *testing.T, withInit, initInRef, bodyInRef bool) (*cell.Cell, *tlb.InternalMessage) {
	t.Helper()

	src := address.MustParseRawAddr("0:" + "11" + "00000000000000000000000000000000000000000000000000000000000000"[:62])
	dst := address.MustParseRawAddr("0:" + "22" + "00000000000000000000000000000000000000000000000000000000000000"[:62])

	body := cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake("hi").EndCell()

	var stateInit *tlb.StateInit
	if withInit {
		stateInit = &tlb.StateInit{
			Code: cell.BeginCell().MustStoreUInt(0xDEAD, 16).EndCell(),
			Data: cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell(),
		}
	}

	msg := &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     src,
		DstAddr:     dst,
		Amount:      tlb.MustFromNano(big.NewInt(1_000_000_000), 9),
		CreatedLT:   42,
		CreatedAt:   1700000000,
		StateInit:   stateInit,
		Body:        body,
	}

	b := cell.BeginCell()
	b.MustStoreBoolBit(false)
	b.MustStoreBoolBit(msg.IHRDisabled)
	b.MustStoreBoolBit(msg.Bounce)
	b.MustStoreBoolBit(msg.Bounced)
	if err := b.StoreAddr(msg.SrcAddr); err != nil {
		t.Fatal(err)
	}
	if err := b.StoreAddr(msg.DstAddr); err != nil {
		t.Fatal(err)
	}
	if err := b.StoreBigCoins(msg.Amount.Nano()); err != nil {
		t.Fatal(err)
	}
	b.MustStoreBoolBit(false) // no extra currencies
	if err := b.StoreBigCoins(msg.IHRFee.Nano()); err != nil {
		t.Fatal(err)
	}
	if err := b.StoreBigCoins(msg.FwdFee.Nano()); err != nil {
		t.Fatal(err)
	}
	b.MustStoreUInt(msg.CreatedLT, 64)
	b.MustStoreUInt(uint64(msg.CreatedAt), 32)

	if stateInit == nil {
		b.MustStoreBoolBit(false)
	} else {
		initCell, err := stateInit.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		b.MustStoreBoolBit(true)
		b.MustStoreBoolBit(initInRef)
		if initInRef {
			b.MustStoreRef(initCell)
		} else {
			b.MustStoreBuilder(initCell.ToBuilder())
		}
	}

	b.MustStoreBoolBit(bodyInRef)
	if bodyInRef {
		b.MustStoreRef(body)
	} else {
		b.MustStoreBuilder(body.ToBuilder())
	}

	return b.EndCell(), msg
}

// A ref-stored body or StateInit is what wallets and the reference node
// normally emit, and both Either branches are valid TL-B, so all four
// combinations must be accepted.
func TestPrepareParsedMessageAcceptsBothEitherLayouts(t *testing.T) {
	for _, test := range []struct {
		name      string
		withInit  bool
		initInRef bool
		bodyInRef bool
	}{
		{name: "no_init_body_inline"},
		{name: "no_init_body_ref", bodyInRef: true},
		{name: "init_inline_body_inline", withInit: true},
		{name: "init_inline_body_ref", withInit: true, bodyInRef: true},
		{name: "init_ref_body_inline", withInit: true, initInRef: true},
		{name: "init_ref_body_ref", withInit: true, initInRef: true, bodyInRef: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			msgCell, _ := buildLayoutMessageCell(t, test.withInit, test.initInRef, test.bodyInRef)

			prepared, err := PrepareMessage(msgCell)
			if err != nil {
				t.Fatalf("PrepareMessage rejected a valid layout: %v", err)
			}

			if _, err = PrepareParsedMessage(msgCell, prepared.Message()); err != nil {
				t.Fatalf("PrepareParsedMessage rejected a valid layout: %v", err)
			}
		})
	}
}

// The guard must still catch a parsed form that does not describe the cell,
// which is the whole reason it exists.
func TestPrepareParsedMessageRejectsDivergentParsedForm(t *testing.T) {
	msgCell, _ := buildLayoutMessageCell(t, true, true, true)

	for _, test := range []struct {
		name   string
		mutate func(*tlb.InternalMessage)
	}{
		{
			name:   "amount",
			mutate: func(m *tlb.InternalMessage) { m.Amount = tlb.MustFromNano(big.NewInt(2), 9) },
		},
		{
			name:   "bounce",
			mutate: func(m *tlb.InternalMessage) { m.Bounce = !m.Bounce },
		},
		{
			name:   "created_lt",
			mutate: func(m *tlb.InternalMessage) { m.CreatedLT++ },
		},
		{
			name: "src_addr",
			mutate: func(m *tlb.InternalMessage) {
				m.SrcAddr = address.MustParseRawAddr("0:" + "33" + "00000000000000000000000000000000000000000000000000000000000000"[:62])
			},
		},
		{
			name: "body",
			mutate: func(m *tlb.InternalMessage) {
				m.Body = cell.BeginCell().MustStoreUInt(1, 32).EndCell()
			},
		},
		{
			name: "state_init",
			mutate: func(m *tlb.InternalMessage) {
				m.StateInit = &tlb.StateInit{Data: cell.BeginCell().MustStoreUInt(1, 8).EndCell()}
			},
		},
		{
			name:   "state_init_dropped",
			mutate: func(m *tlb.InternalMessage) { m.StateInit = nil },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := PrepareMessage(msgCell)
			if err != nil {
				t.Fatal(err)
			}
			tampered := *prepared.Message().AsInternal()
			test.mutate(&tampered)

			if _, err = PrepareParsedMessage(msgCell, &tlb.Message{
				MsgType: tlb.MsgTypeInternal,
				Msg:     &tampered,
			}); err == nil {
				t.Fatal("a parsed message diverging from the cell was accepted")
			}
		})
	}
}

func BenchmarkPrepareParsedMessage(b *testing.B) {
	msgCell, _ := buildLayoutMessageCell(&testing.T{}, false, false, true)
	prepared, err := PrepareMessage(msgCell)
	if err != nil {
		b.Fatal(err)
	}
	msg := prepared.Message()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := PrepareParsedMessage(msgCell, msg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrepareMessageFromCell(b *testing.B) {
	msgCell, _ := buildLayoutMessageCell(&testing.T{}, false, false, true)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := PrepareMessage(msgCell); err != nil {
			b.Fatal(err)
		}
	}
}
