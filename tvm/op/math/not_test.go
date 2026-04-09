package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestNOT(t *testing.T) {
	st := vm.NewStack()
	if err := st.PushInt(big.NewInt(0x0F)); err != nil {
		t.Fatalf("failed to push int: %v", err)
	}

	if err := NOT().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("NOT failed: %v", err)
	}

	got, err := st.PopIntFinite()
	if err != nil {
		t.Fatalf("failed to pop result: %v", err)
	}
	if got.Cmp(big.NewInt(-16)) != 0 {
		t.Fatalf("unexpected NOT result: %s", got.String())
	}
}
