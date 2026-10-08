package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionStateInitLibraryWalkStopsAtBudget(t *testing.T) {
	const levels = 12
	// Thirteen physical cells encode 4096 leaves by sharing both fork refs.
	// The reference charges every entered node, including repeated refs.
	payload := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	root := cell.BeginCell().MustStoreUInt(0b111, 3).MustStoreUInt(256-levels, 8).
		MustStoreBoolBit(false).MustStoreRef(payload).EndCell()
	for range levels {
		root = cell.BeginCell().MustStoreUInt(0, 2).MustStoreRef(root).MustStoreRef(root).EndCell()
	}
	loads := 0
	var trace *cell.Trace
	trace = cell.NewTrace(cell.TraceHooks{
		OnLoad: func(c *cell.Cell) {
			loads++
			if c.HashKey() == payload.HashKey() {
				t.Fatal("library payload was traversed")
			}
		},
		OnChild: func(int) *cell.Trace { return trace },
	})
	msg := &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{
		StateInit: &tlb.StateInit{Lib: root.AsDict(256).SetTrace(trace)},
	}}
	if err := transactionValidateMessageStateInitLibs(msg); err == nil {
		t.Fatal("oversized dictionary was accepted")
	}
	if loads > 1024 || loads == 0 {
		t.Fatalf("dictionary loads=%d, want 1..1024", loads)
	}
}

func TestTransactionStateInitLibraryWalkPayloadsRemainOpaque(t *testing.T) {
	payload := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().WithTrace(cell.NewTrace(cell.TraceHooks{
		OnLoad: func(*cell.Cell) { t.Fatal("opaque state payload was loaded") },
	}))
	root := cell.BeginCell().MustStoreUInt(0b111, 3).MustStoreUInt(256, 9).
		MustStoreBoolBit(false).MustStoreRef(payload).EndCell()
	msg := &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{
		StateInit: &tlb.StateInit{Code: payload, Data: payload, Lib: root.AsDict(256)},
	}}
	if err := transactionValidateMessageStateInitLibs(msg); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionStateInitLibraryWalkLabelsAndStrongValidation(t *testing.T) {
	payload := cell.BeginCell().EndCell()
	short := cell.BeginCell().MustStoreBoolBit(false)
	for range 256 {
		short.MustStoreBoolBit(true)
	}
	short.MustStoreBoolBit(false).MustStoreSlice(make([]byte, 32), 256)
	long := cell.BeginCell().MustStoreUInt(2, 2).MustStoreUInt(256, 9).MustStoreSlice(make([]byte, 32), 256)
	same := cell.BeginCell().MustStoreUInt(6, 3).MustStoreUInt(256, 9)
	for _, label := range []*cell.Builder{short, long, same} {
		root := label.MustStoreBoolBit(false).MustStoreRef(payload).EndCell()
		msg := &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{StateInit: &tlb.StateInit{Lib: root.AsDict(256)}}}
		if err := transactionValidateMessageStateInitLibs(msg); err != nil {
			t.Fatalf("valid label rejected: %v", err)
		}
	}
	valid := same.EndCell()
	proof, err := valid.CreateProof(cell.CreateProofSkeleton())
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []*cell.Cell{
		cell.BeginCell().EndCell(),
		cell.BeginCell().MustStoreUInt(6, 3).MustStoreUInt(257, 9).MustStoreBoolBit(false).MustStoreRef(payload).EndCell(),
		cell.BeginCell().MustStoreUInt(2, 2).MustStoreUInt(256, 9).MustStoreBoolBit(false).MustStoreRef(payload).EndCell(),
		cell.BeginCell().MustStoreUInt(0, 2).MustStoreRef(valid).EndCell(),
		proof,
	} {
		msg := &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{StateInit: &tlb.StateInit{Lib: root.AsDict(256)}}}
		if err := transactionValidateMessageStateInitLibs(msg); err == nil {
			t.Fatal("invalid or special dictionary node was accepted")
		}
	}
}
