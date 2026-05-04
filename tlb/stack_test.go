package tlb

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestStack_ToCell(t *testing.T) {
	ref := cell.BeginCell().MustStoreInt(-777, 12).EndCell()

	s := NewStack()
	s.Push([]any{})
	s.Push([]any{int64(222)})
	s.Push([]any{int64(555), StackNaN{}})
	s.Push(StackNaN{})
	s.Push(cell.BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(ref))
	s.Push(cell.BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(ref).EndCell())
	s.Push(uint64(18446744073709551615))
	s.Push(int64(555))
	s.Push(cell.BeginCell().MustStoreUInt(0x55, 8).MustStoreRef(ref).EndCell().BeginParse())

	c, err := s.ToCell()
	if err != nil {
		t.Fatal("failed to cell", err)
	}

	var s2 Stack
	err = s2.LoadFromCell(c.BeginParse())
	if err != nil {
		t.Fatal("failed from cell", err)
	}

	c2, err := s2.ToCell()
	if err != nil {
		t.Fatal("failed to cell", err)
	}

	if !bytes.Equal(c2.Hash(), c.Hash()) {
		t.Fatal("rebuild not same", err)
	}

	v, err := s2.Pop()
	if err != nil {
		t.Fatal("pop 1 err", err)
	}
	slc := v.(*cell.Slice)
	if slc.MustLoadUInt(8) != 0x55 {
		t.Fatal("slice val err", err)
	}
	if !bytes.Equal(slc.MustLoadRef().MustToCell().Hash(), ref.Hash()) {
		t.Fatal("slice ref err", err)
	}

	v, err = s2.Pop()
	if err != nil {
		t.Fatal("pop 1 err", err)
	}
	if v.(*big.Int).Uint64() != 555 {
		t.Fatal("tiny val err", err)
	}

	v, err = s2.Pop()
	if err != nil {
		t.Fatal("pop 1 err", err)
	}
	if v.(*big.Int).Uint64() != 18446744073709551615 {
		t.Fatal("big val err", err)
	}

	s2.Pop()
	s2.Pop()
	s2.Pop()

	v, err = s2.Pop()
	if err != nil {
		t.Fatal("pop 0 err", err)
	}
	if v.([]any)[0].(*big.Int).Uint64() != 555 {
		t.Fatal("tuple big val err", err)
	}

	v, err = s2.Pop()
	if err != nil {
		t.Fatal("pop 0 err", err)
	}
	if v.([]any)[0].(*big.Int).Uint64() != 222 {
		t.Fatal("tuple big val err", err)
	}

	v, err = s2.Pop()
	if err != nil {
		t.Fatal("pop 0 err", err)
	}
	if len(v.([]any)) != 0 {
		t.Fatal("tuple val err", err)
	}
}

func TestStack_Depth(t *testing.T) {
	stack := NewStack()
	stack.Push(struct{}{})
	stack.Push(struct{}{})
	stack.Push(struct{}{})
	stack.Push(struct{}{})
	dep := stack.Depth()
	if dep != 4 {
		t.Errorf("bad stack depth")
	}
}

func TestParseStackValue(t *testing.T) {
	boc, _ := hex.DecodeString("b5ee9c724101140100be00010607000101020607000a021302000304020005060109040010b020070200080902060700020a13004380115c9efc1f3c4944ec3bc823fa372c3bff194d21a5bf9a0b1a35ea8acc5e8b7e5002000b0c00440200a6e7d0f36eaebcc9b69f116cd65416c8703d9ea2e1e663692fdd3f9c166d41740012010000000005c8f38202000d0c001201000028bd7718211002000e0f020010110012010000000065d8d26802001211001201000000000000000000120100000000076ef1ea00020039c2bf52")
	c, _ := cell.FromBOC(boc)

	vl, err := ParseStackValue(c.BeginParse())
	if err != nil {
		t.Fatal(err.Error())
	}

	b := cell.BeginCell()
	err = SerializeStackValue(b, vl)
	if err != nil {
		t.Fatal(err.Error())
	}

	if !bytes.Equal(b.EndCell().Hash(), c.Hash()) {
		t.Fatal("rebuild not same", err)
	}
}

func TestStackSliceValueUsesBaseCellOffsets(t *testing.T) {
	refA := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	refB := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	base := cell.BeginCell().
		MustStoreUInt(0xABCD, 16).
		MustStoreRef(refA).
		MustStoreRef(refB).
		EndCell()

	value := cell.BeginCell().
		MustStoreUInt(0x04, 8).
		MustStoreRef(base).
		MustStoreUInt(4, 10).
		MustStoreUInt(12, 10).
		MustStoreUInt(1, 3).
		MustStoreUInt(2, 3).
		EndCell()

	parsed, err := ParseStackValue(value.BeginParse())
	if err != nil {
		t.Fatalf("parse stack slice: %v", err)
	}
	slc := parsed.(*cell.Slice)
	check := slc.Copy()
	if got := check.MustLoadUInt(8); got != 0xBC {
		t.Fatalf("slice bits = %#x, want 0xbc", got)
	}
	if got := check.MustLoadRef().MustToCell().Hash(); !bytes.Equal(got, refB.Hash()) {
		t.Fatal("slice ref was not restored from the base-cell offset")
	}

	roundTrip := cell.BeginCell()
	if err = SerializeStackValue(roundTrip, parsed); err != nil {
		t.Fatalf("serialize stack slice: %v", err)
	}
	if !bytes.Equal(roundTrip.EndCell().Hash(), value.Hash()) {
		t.Fatal("slice stack value should preserve base cell offsets")
	}
}

func TestStackTupleSliceValuePreservesOffsets(t *testing.T) {
	ref := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	base := cell.BeginCell().MustStoreUInt(0x1234, 16).MustStoreRef(ref).EndCell()
	slc, err := base.BeginParse().PreloadSubslice(12, 1)
	if err != nil {
		t.Fatalf("build tuple slice: %v", err)
	}
	if err = slc.Advance(4); err != nil {
		t.Fatalf("advance tuple slice: %v", err)
	}

	value := cell.BeginCell()
	if err = SerializeStackValue(value, []any{slc}); err != nil {
		t.Fatalf("serialize tuple: %v", err)
	}
	parsed, err := ParseStackValue(value.EndCell().BeginParse())
	if err != nil {
		t.Fatalf("parse tuple: %v", err)
	}
	tup := parsed.([]any)
	if len(tup) != 1 {
		t.Fatalf("tuple len = %d, want 1", len(tup))
	}
	gotSlice := tup[0].(*cell.Slice)
	start, end := gotSlice.BitRange()
	if start != 4 || end != 12 {
		t.Fatalf("tuple slice bit range = %d..%d, want 4..12", start, end)
	}
	if got := gotSlice.MustLoadUInt(8); got != 0x23 {
		t.Fatalf("tuple slice bits = %#x, want 0x23", got)
	}
}

func TestNewStackFromVMSerializesNaN(t *testing.T) {
	vmStack := vm.NewStack()
	overflow := new(big.Int).Lsh(big.NewInt(1), 257)
	if err := vmStack.PushIntQuiet(overflow); err != nil {
		t.Fatalf("push quiet overflow: %v", err)
	}

	stack, err := NewStackFromVM(vmStack)
	if err != nil {
		t.Fatalf("stack from vm: %v", err)
	}

	cellValue, err := stack.ToCell()
	if err != nil {
		t.Fatalf("stack to cell: %v", err)
	}

	var parsed Stack
	if err = parsed.LoadFromCell(cellValue.BeginParse()); err != nil {
		t.Fatalf("stack from cell: %v", err)
	}

	val, err := parsed.Pop()
	if err != nil {
		t.Fatalf("pop serialized value: %v", err)
	}
	if _, ok := val.(StackNaN); !ok {
		t.Fatalf("expected StackNaN after serialization, got %T", val)
	}
}
