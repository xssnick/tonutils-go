package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestPLDUReturnsOnlyPreloadedInteger(t *testing.T) {
	st := vm.NewStack()
	src := cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()
	if err := st.PushSlice(src); err != nil {
		t.Fatalf("failed to push slice: %v", err)
	}

	if err := PLDU(8).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("PLDU failed: %v", err)
	}

	if st.Len() != 1 {
		t.Fatalf("unexpected stack depth: %d", st.Len())
	}

	val, err := st.PopInt()
	if err != nil {
		t.Fatalf("failed to pop int: %v", err)
	}

	if val.Cmp(big.NewInt(0xAB)) != 0 {
		t.Fatalf("unexpected value: %s", val.String())
	}
}
