package dict

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestExecSubdictKeepsPrefixWhenRequested(t *testing.T) {
	root := mustPlainDictRoot(t, 4, map[uint64]uint64{
		0b1000: 0x11,
		0b1010: 0x22,
		0b0111: 0x33,
	}, 8)

	state := newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b10, 2)); err != nil {
		t.Fatalf("push kept-prefix slice: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push kept-prefix bits: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push kept-prefix root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push kept-prefix key bits: %v", err)
	}
	if err := execSubdict(false)(dictScalarVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("subdict without prefix removal failed: %v", err)
	}

	subdictRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop kept-prefix root: %v", err)
	}
	if subdictRoot == nil {
		t.Fatal("expected kept-prefix subdict root")
	}

	gotA, err := subdictRoot.AsDict(4).LoadValue(mustDictKeyCell(t, 0b1000, 4))
	if err != nil || gotA.MustLoadUInt(8) != 0x11 {
		t.Fatalf("unexpected kept-prefix first value: %v err=%v", gotA, err)
	}
	gotB, err := subdictRoot.AsDict(4).LoadValue(mustDictKeyCell(t, 0b1010, 4))
	if err != nil || gotB.MustLoadUInt(8) != 0x22 {
		t.Fatalf("unexpected kept-prefix second value: %v err=%v", gotB, err)
	}
	if _, err = subdictRoot.AsDict(4).LoadValue(mustDictKeyCell(t, 0b0111, 4)); err == nil {
		t.Fatal("expected unrelated key to be absent from kept-prefix subdict")
	}
}

func TestExecSubdictMissingPrefixReturnsEmptyRoot(t *testing.T) {
	root := mustPlainDictRoot(t, 4, map[uint64]uint64{
		0b1000: 0x11,
	}, 8)

	state := newDictTestState()
	if err := state.Stack.PushSlice(mustDictKeySlice(t, 0b01, 2)); err != nil {
		t.Fatalf("push missing-prefix slice: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push missing-prefix bits: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push missing-prefix root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push missing-prefix key bits: %v", err)
	}
	if err := execSubdict(true)(dictScalarVariant{kind: dictKeySlice})(state); err != nil {
		t.Fatalf("subdict miss should return an empty root: %v", err)
	}

	subdictRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop missing-prefix root: %v", err)
	}
	if subdictRoot != nil {
		t.Fatalf("expected empty subdict root for missing prefix, got %v", subdictRoot)
	}
}

func TestUnsignedAndSignedSubdictPrefixesAdditionalBranches(t *testing.T) {
	state := newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push oversized signed prefix value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push oversized signed prefix bits: %v", err)
	}
	_, _, err := popSubdictPrefix(state, 8, dictKeySignedInt)
	if err == nil {
		t.Fatal("expected signed prefix overflow")
	}
	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellUnderflow {
		t.Fatalf("expected cell underflow for oversized signed prefix, got %v", err)
	}

	root := mustPlainDictRoot(t, 8, map[uint64]uint64{
		0xF0: 0x11,
		0xF3: 0x22,
		0xA0: 0x33,
	}, 8)

	state = newDictTestState()
	if err := state.Stack.PushInt(big.NewInt(0xF)); err != nil {
		t.Fatalf("push unsigned prefix value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push unsigned prefix bits: %v", err)
	}
	if err := state.Stack.PushCell(root); err != nil {
		t.Fatalf("push unsigned subdict root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push unsigned subdict key bits: %v", err)
	}
	if err := execSubdict(false)(dictScalarVariant{kind: dictKeyUnsignedInt})(state); err != nil {
		t.Fatalf("unsigned subdict without prefix removal failed: %v", err)
	}

	subdictRoot, err := state.Stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("pop unsigned subdict root: %v", err)
	}
	if subdictRoot == nil {
		t.Fatal("expected unsigned subdict root")
	}
	gotA, err := subdictRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0xF0))
	if err != nil || gotA.MustLoadUInt(8) != 0x11 {
		t.Fatalf("unexpected unsigned subdict first value: %v err=%v", gotA, err)
	}
	gotB, err := subdictRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0xF3))
	if err != nil || gotB.MustLoadUInt(8) != 0x22 {
		t.Fatalf("unexpected unsigned subdict second value: %v err=%v", gotB, err)
	}
	if _, err = subdictRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0xA0)); err == nil {
		t.Fatal("expected non-matching unsigned key to be absent from subdict")
	}
}
