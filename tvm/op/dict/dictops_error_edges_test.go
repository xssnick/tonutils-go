package dict

import (
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestExecStoreDictAndSkipDictErrorBranches(t *testing.T) {
	t.Run("store dict builder underflow", func(t *testing.T) {
		if err := execStoreDict(newDictTestState()); err == nil {
			t.Fatal("expected execStoreDict to fail on missing builder")
		}
	})

	t.Run("store dict missing root", func(t *testing.T) {
		state := newDictTestState()
		if err := state.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("push builder without dict root: %v", err)
		}
		if err := execStoreDict(state); err == nil {
			t.Fatal("expected execStoreDict to fail on missing dict root")
		}
	})

	t.Run("store dict overflow", func(t *testing.T) {
		state := newDictTestState()
		full := cell.BeginCell()
		for i := 0; i < 4; i++ {
			full.MustStoreRef(cell.BeginCell().EndCell())
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push dict root: %v", err)
		}
		if err := state.Stack.PushBuilder(full); err != nil {
			t.Fatalf("push full builder: %v", err)
		}
		err := execStoreDict(state)
		if err == nil {
			t.Fatal("expected execStoreDict to fail on builder ref overflow")
		}
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellOverflow {
			t.Fatalf("expected cell overflow, got %v", err)
		}
	})

	t.Run("skip dict missing slice", func(t *testing.T) {
		if err := execSkipDict(newDictTestState()); err == nil {
			t.Fatal("expected execSkipDict to fail on missing slice")
		}
	})

	t.Run("skip dict invalid serialization", func(t *testing.T) {
		state := newDictTestState()
		if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
			t.Fatalf("push invalid dict serialization: %v", err)
		}
		err := execSkipDict(state)
		if err == nil {
			t.Fatal("expected execSkipDict to fail on invalid serialization")
		}
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellUnderflow {
			t.Fatalf("expected cell underflow, got %v", err)
		}
	})
}

func TestPFXDICTSWITCHDeserializeEdgeBranches(t *testing.T) {
	t.Run("short prefix", func(t *testing.T) {
		op := PFXDICTSWITCH(nil)
		if err := op.Deserialize(cell.BeginCell().EndCell().BeginParse()); err == nil {
			t.Fatal("expected Deserialize to fail on short opcode prefix")
		}
	})

	t.Run("missing root ref", func(t *testing.T) {
		op := PFXDICTSWITCH(nil)
		code := cell.BeginCell().
			MustStoreSlice([]byte{0xF4, 0xAC}, 13).
			MustStoreBoolBit(true).
			MustStoreUInt(1, 10).
			EndCell().BeginParse()
		if err := op.Deserialize(code); err == nil {
			t.Fatal("expected Deserialize to fail when root flag is set without a ref")
		}
	})

	t.Run("missing bits", func(t *testing.T) {
		op := PFXDICTSWITCH(nil)
		code := cell.BeginCell().
			MustStoreSlice([]byte{0xF4, 0xAC}, 13).
			MustStoreBoolBit(false).
			EndCell().BeginParse()
		if err := op.Deserialize(code); err == nil {
			t.Fatal("expected Deserialize to fail when key bits are truncated")
		}
	})

	t.Run("nil root serialization", func(t *testing.T) {
		op := PFXDICTSWITCH(cell.BeginCell().EndCell(), 1)
		code := cell.BeginCell().
			MustStoreSlice([]byte{0xF4, 0xAC}, 13).
			MustStoreBoolBit(false).
			MustStoreUInt(5, 10).
			EndCell().BeginParse()
		if err := op.Deserialize(code); err != nil {
			t.Fatalf("Deserialize with nil root flag failed: %v", err)
		}
		if op.root != nil || op.bits != 5 {
			t.Fatalf("expected Deserialize to clear the root and keep bits=5, got root=%v bits=%d", op.root, op.bits)
		}
		if got := op.SerializeText(); got != "PFXDICTSWITCH 5 (<nil>)" {
			t.Fatalf("unexpected nil-root text form: %q", got)
		}
	})

	t.Run("interpret missing input", func(t *testing.T) {
		if err := PFXDICTSWITCH(nil, 4).Interpret(newDictTestState()); err == nil {
			t.Fatal("expected Interpret to fail on missing input slice")
		}
	})
}
