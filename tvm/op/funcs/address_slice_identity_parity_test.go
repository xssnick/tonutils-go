package funcs

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestRewriteVarAddrFullAnycastPrefixPreservesSourceCell(t *testing.T) {
	addrCell := cell.BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(4, 5).
		MustStoreUInt(0b1010, 4).
		MustStoreUInt(4, 9).
		MustStoreInt(0, 32).
		MustStoreUInt(0, 4).
		EndCell()
	state := newFuncTestState(t, nil)
	state.GlobalVersion = 9
	if err := state.Stack.PushSlice(addrCell.MustBeginParse()); err != nil {
		t.Fatalf("push address: %v", err)
	}

	if err := REWRITEVARADDR().Interpret(state); err != nil {
		t.Fatalf("REWRITEVARADDR failed: %v", err)
	}
	rewritten, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop rewritten address: %v", err)
	}
	if rewritten.BitsLeft() != 4 {
		t.Fatalf("rewritten address bits = %d, want 4", rewritten.BitsLeft())
	}
	if !bytes.Equal(rewritten.BaseCell().Hash(), addrCell.Hash()) {
		t.Fatal("full-width anycast rewrite did not preserve the source cell")
	}
}
