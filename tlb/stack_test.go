package tlb

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestStack_ToCell(t *testing.T) {
	ref := cell.BeginCell().MustStoreInt(-777, 12).EndCell()

	s := NewStack()
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
