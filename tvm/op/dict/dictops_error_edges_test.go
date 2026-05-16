package dict

import (
	"errors"
	"math/big"
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

	t.Run("store dict defers depth overflow to cell finalization", func(t *testing.T) {
		state := newDictTestState()
		deep := makeDictTestDepth1024Cell(t)
		if err := state.Stack.PushCell(deep); err != nil {
			t.Fatalf("push deep dict root: %v", err)
		}
		if err := state.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("push builder: %v", err)
		}
		if err := execStoreDict(state); err != nil {
			t.Fatalf("storing depth-1024 dict root should be deferred: %v", err)
		}
		builder, err := state.Stack.PopBuilder()
		if err != nil {
			t.Fatalf("pop builder: %v", err)
		}
		if _, err = builder.EndCellSpecial(false); !errors.Is(err, cell.ErrCellDepthLimit) {
			t.Fatalf("expected finalization depth error, got %v", err)
		}
	})

	t.Run("dict set ref maps depth overflow to cell overflow", func(t *testing.T) {
		state := newDictTestState()
		if err := state.Stack.PushCell(makeDictTestDepth1024Cell(t)); err != nil {
			t.Fatalf("push deep value ref: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push key: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push empty root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key width: %v", err)
		}

		err := execDictSet(cell.DictSetModeSet)(dictValueVariant{kind: dictKeyUnsignedInt, byRef: true})(state)
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellOverflow {
			t.Fatalf("expected cell overflow, got %v", err)
		}
	})

	t.Run("dict set builder maps depth overflow to cell overflow", func(t *testing.T) {
		state := newDictTestState()
		value := cell.BeginCell()
		if err := value.StoreRefUncheckedDepth(makeDictTestDepth1024Cell(t)); err != nil {
			t.Fatalf("store deep value ref: %v", err)
		}
		if err := state.Stack.PushBuilder(value); err != nil {
			t.Fatalf("push deep value builder: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push key: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push empty root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push key width: %v", err)
		}

		err := execDictSetBuilder(cell.DictSetModeSet)(dictScalarVariant{kind: dictKeyUnsignedInt})(state)
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

func makeDictTestDepth1024Cell(t *testing.T) *cell.Cell {
	t.Helper()

	root := cell.BeginCell().EndCell()
	for i := 0; i < 1024; i++ {
		root = cell.BeginCell().MustStoreRef(root).EndCell()
	}
	if root.Depth() != 1024 {
		t.Fatalf("unexpected test cell depth: %d", root.Depth())
	}
	return root
}

func TestPFXDICTSWITCHDeserializeEdgeBranches(t *testing.T) {
	t.Run("short prefix", func(t *testing.T) {
		op := PFXDICTSWITCH(nil)
		if err := op.Deserialize(cell.BeginCell().EndCell().MustBeginParse()); err == nil {
			t.Fatal("expected Deserialize to fail on short opcode prefix")
		}
	})

	t.Run("missing root ref", func(t *testing.T) {
		op := PFXDICTSWITCH(nil)
		code := cell.BeginCell().
			MustStoreSlice([]byte{0xF4, 0xAC}, 13).
			MustStoreBoolBit(true).
			MustStoreUInt(1, 10).
			EndCell().MustBeginParse()
		if err := op.Deserialize(code); err == nil {
			t.Fatal("expected Deserialize to fail when root flag is set without a ref")
		}
	})

	t.Run("missing bits", func(t *testing.T) {
		op := PFXDICTSWITCH(nil)
		code := cell.BeginCell().
			MustStoreSlice([]byte{0xF4, 0xAC}, 13).
			MustStoreBoolBit(false).
			MustStoreRef(cell.BeginCell().EndCell()).
			EndCell().MustBeginParse()
		if err := op.Deserialize(code); err == nil {
			t.Fatal("expected Deserialize to fail when key bits are truncated")
		}
	})

	t.Run("nil root flag with ref decodes as empty switch", func(t *testing.T) {
		op := PFXDICTSWITCH(cell.BeginCell().EndCell(), 1)
		code := cell.BeginCell().
			MustStoreSlice([]byte{0xF4, 0xAC}, 13).
			MustStoreBoolBit(false).
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreUInt(5, 10).
			EndCell().MustBeginParse()
		if err := op.Deserialize(code); err != nil {
			t.Fatalf("Deserialize with nil root flag and ref failed: %v", err)
		}
		if op.root != nil || op.bits != 5 {
			t.Fatalf("unexpected decoded switch: root=%v bits=%d", op.root, op.bits)
		}
	})

	t.Run("interpret missing input", func(t *testing.T) {
		if err := PFXDICTSWITCH(nil, 4).Interpret(newDictTestState()); err == nil {
			t.Fatal("expected Interpret to fail on missing input slice")
		}
	})
}
