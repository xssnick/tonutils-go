package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func Test_DUP2(t *testing.T) {
	st := vm.NewStack()

	st.PushAny(big.NewInt(1))
	st.PushAny(big.NewInt(2))
	dup2 := DUP2()

	err := dup2.Interpret(&vm.State{
		Stack: st,
	})
	if err != nil {
		t.Fatal("Failed ROT interpretation:", err.Error())
	}

	b, err := st.PopInt()
	if err != nil || b.Cmp(big.NewInt(2)) != 0 {
		t.Errorf("Expected 2 at b, got %v", b)
	}
	a, err := st.PopInt()
	if err != nil || a.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("Expected 1 at a, got %v", a)
	}
	b, err = st.PopInt()
	if err != nil || b.Cmp(big.NewInt(2)) != 0 {
		t.Errorf("Expected 2 at b, got %v", b)
	}
	a, err = st.PopInt()
	if err != nil || a.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("Expected 1 at a, got %v", a)
	}

}
