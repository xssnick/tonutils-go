package cellslice

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestSDSKIPFIRSTReturnsSliceTail(t *testing.T) {
	src := cell.BeginCell().MustStoreUInt(0xABCD, 16).ToSlice()

	st := vm.NewStack()
	if err := st.PushSlice(src); err != nil {
		t.Fatalf("failed to push slice: %v", err)
	}
	if err := st.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("failed to push int: %v", err)
	}

	if err := SDSKIPFIRST().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("SDSKIPFIRST failed: %v", err)
	}

	if st.Len() != 1 {
		t.Fatalf("unexpected stack depth: %d", st.Len())
	}

	got, err := st.PopSlice()
	if err != nil {
		t.Fatalf("failed to pop slice: %v", err)
	}

	want := cell.BeginCell().MustStoreUInt(0xCD, 8).ToSlice()
	if !bytes.Equal(got.MustToCell().Hash(), want.MustToCell().Hash()) {
		t.Fatalf("unexpected tail slice: got=%s want=%s", got.MustToCell().Dump(), want.MustToCell().Dump())
	}
}
