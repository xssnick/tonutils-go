package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// The old generated OutListNode schema (TON 9f008b129f1f) spends one
// shared 1024-cell budget on action/message roots and typed dictionary refs.
// Modern StateInitWithLibs validation receives a separate budget.
func TestHistoricalActionStateInitLibraryBudget(t *testing.T) {
	for _, libraries := range []int{511, 512, 513} {
		libs := transactionStateInitBudgetLibraries(t, libraries)
		for _, currencies := range []int{0, 1, 2} {
			extra := transactionHistoricalBudgetCurrencies(t, currencies)
			for _, referenced := range []bool{false, true} {
				for _, bodyRef := range []bool{false, true} {
					t.Run(fmt.Sprintf("libs%d/extra%d/ref%t/bodyref%t", libraries, currencies, referenced, bodyRef), func(t *testing.T) {
						message := transactionHistoricalBudgetMessage(t, libs, extra, referenced, bodyRef)
						var parsed tlb.Message
						if err := transactionParseCell(&parsed, message); err != nil {
							t.Fatal(err)
						}
						cost := 2 + 2*libraries - 1
						if currencies > 0 {
							cost += 2*currencies - 1
						}
						if referenced {
							cost++
						}
						if bodyRef {
							cost++
						}
						if err := transactionValidateActionMessageBudget(message, &parsed, true); (err == nil) != (cost <= 1024) {
							t.Fatalf("historical validation cost=%d, err=%v", cost, err)
						}
						if got := transactionOutboundActionMessageStructureValid(message, true); got != (cost <= 1024) {
							t.Fatalf("historical action prepass=%t for cost=%d", got, cost)
						}
						if !transactionOutboundActionMessageStructureValid(message, false) {
							t.Fatal("modern action prepass rejected an opaque library dictionary")
						}
						if err := transactionValidateMessageStateInitLibs(&parsed); (err == nil) != (libraries <= 512) {
							t.Fatalf("separate library validation err=%v", err)
						}
					})
				}
			}
		}
	}
}

func TestHistoricalActionStateInitLibraryBudgetStopsSharedBranches(t *testing.T) {
	for _, extra := range []bool{false, true} {
		t.Run(fmt.Sprintf("extra_%t", extra), func(t *testing.T) {
			const levels = 12
			keyBits := uint(256)
			value := cell.BeginCell().MustStoreBoolBit(false).MustStoreRef(cell.BeginCell().EndCell())
			if extra {
				keyBits = 32
				value = cell.BeginCell().MustStoreUInt(1, 5).MustStoreUInt(1, 8)
			}
			lengthBits := uint(8)
			if extra {
				lengthBits = 5
			}
			root := cell.BeginCell().MustStoreUInt(6, 3).MustStoreUInt(uint64(keyBits-levels), lengthBits).
				MustStoreBuilder(value).EndCell()
			nodes := map[cell.Hash]bool{root.HashKey(): true}
			for range levels {
				root = cell.BeginCell().MustStoreUInt(0, 2).MustStoreRef(root).MustStoreRef(root).EndCell()
				nodes[root.HashKey()] = true
			}
			libs, currencies := root.AsDict(256), cell.NewDict(32)
			if extra {
				libs, currencies = nil, root.AsDict(32)
			}
			message := transactionHistoricalBudgetMessage(t, libs, currencies, true, true)
			loads := 0
			var trace *cell.Trace
			trace = cell.NewTrace(cell.TraceHooks{
				OnLoad: func(c *cell.Cell) {
					if nodes[c.HashKey()] {
						loads++
					}
				},
				OnChild: func(int) *cell.Trace { return trace },
			})
			message = message.WithTrace(trace)
			if transactionOutboundActionMessageStructureValid(message, true) {
				t.Fatal("historical action prepass accepted oversized dictionary")
			}
			if loads == 0 || loads > 1022 {
				t.Fatalf("historical dictionary loads=%d, want 1..1022", loads)
			}
			if extra {
				loads = 0
				if transactionOutboundActionMessageStructureValid(message, false) {
					t.Fatal("modern action prepass accepted oversized currency dictionary")
				}
				if loads == 0 || loads > 1022 {
					t.Fatalf("modern currency loads=%d, want 1..1022", loads)
				}
			}
			if !extra {
				loads = 0
				if !transactionOutboundActionMessageStructureValid(message, false) {
					t.Fatal("modern action prepass entered opaque library dictionary")
				}
				if loads != 0 {
					t.Fatalf("modern action prepass loaded %d library dictionary nodes", loads)
				}
				if _, err := PrepareMessage(message); err == nil {
					t.Fatal("PrepareMessage accepted oversized library dictionary")
				}
				if loads == 0 || loads > 1024 {
					t.Fatalf("PrepareMessage library loads=%d, want 1..1024", loads)
				}
				var parsed tlb.Message
				if err := transactionParseCell(&parsed, message); err != nil {
					t.Fatal(err)
				}
				loads = 0
				if _, err := PrepareParsedMessage(message, &parsed); err == nil {
					t.Fatal("PrepareParsedMessage accepted oversized library dictionary")
				}
				if loads == 0 || loads > 1024 {
					t.Fatalf("PrepareParsedMessage library loads=%d, want 1..1024", loads)
				}
			}
		})
	}
}

func transactionHistoricalBudgetCurrencies(t *testing.T, count int) *cell.Dictionary {
	t.Helper()
	dict := cell.NewDict(32)
	for i := range count {
		if err := dict.SetIntKey(big.NewInt(int64(i)), cell.BeginCell().MustStoreUInt(1, 5).MustStoreUInt(1, 8).EndCell()); err != nil {
			t.Fatal(err)
		}
	}
	return dict
}

func transactionHistoricalBudgetMessage(t *testing.T, libs, currencies *cell.Dictionary, referenced, bodyRef bool) *cell.Cell {
	t.Helper()
	// Code and body descendants must not consume the remaining budget.
	// Leave data absent so inline state and body still fit with currencies.
	payload := cell.BeginCell().EndCell()
	for range 16 {
		payload = cell.BeginCell().MustStoreRef(payload).EndCell()
	}
	msg := &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{
		IHRDisabled: true, SrcAddr: internalEmulationSrcAddr, DstAddr: tonopsTestAddr,
		ExtraCurrencies: currencies, StateInit: &tlb.StateInit{Lib: libs, Code: payload}, Body: payload,
	}}
	builder := cell.BeginCell()
	if err := tlb.StoreMessageWithLayout(builder, msg, tlb.MessageLayout{StateInitInRef: referenced, BodyInRef: bodyRef}); err != nil {
		t.Fatal(err)
	}
	return builder.EndCell()
}
