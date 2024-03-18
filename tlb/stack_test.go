package tlb

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
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
