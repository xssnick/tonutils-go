package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func Test_XCHG2(t *testing.T) {
	st := vm.NewStack()

	st.PushAny(big.NewInt(1))
	st.PushAny(big.NewInt(2))
	st.PushAny(big.NewInt(3))
	st.PushAny(big.NewInt(4))
	st.PushAny(big.NewInt(5))
	st.PushAny(big.NewInt(6))
	xchg2 := XCHG2(2, 3)

	err := xchg2.Interpret(&vm.State{
		Stack: st,
	})
	if err != nil {
		t.Fatal("Failed ROT interpretation:", err.Error())
	}
	c, err := st.PopInt()
	if err != nil || c.Cmp(big.NewInt(3)) != 0 {
		t.Errorf("Expected 3 at c, got %v", c)
	}
	d, err := st.PopInt()
	if err != nil || d.Cmp(big.NewInt(4)) != 0 {
		t.Errorf("Expected 4 at d, got %v", d)
	}
	e, err := st.PopInt()
	if err != nil || e.Cmp(big.NewInt(5)) != 0 {
		t.Errorf("Expected 5 at e, got %v", e)
	}
	f, err := st.PopInt()
	if err != nil || f.Cmp(big.NewInt(6)) != 0 {
		t.Errorf("Expected 6 at f, got %v", f)
	}
	b, err := st.PopInt()
	if err != nil || b.Cmp(big.NewInt(2)) != 0 {
		t.Errorf("Expected 2 at b, got %v", b)
	}
	a, err := st.PopInt()
	if err != nil || a.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("Expected 1 at a, got %v", a)
	}
}
