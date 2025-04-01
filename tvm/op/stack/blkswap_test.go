package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func Test_BLKSWAP(t *testing.T) {
	st := vm.NewStack()

	st.PushAny(big.NewInt(1))
	st.PushAny(big.NewInt(2))
	st.PushAny(big.NewInt(3))

	blkswap := BLKSWAP(0, 1)

	err := blkswap.Interpret(&vm.State{
		Stack: st,
	})
	if err != nil {
		t.Fatal("Failed ROT interpretation:", err.Error())
	}
	a, err := st.PopInt()
	if err != nil || a.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("Expected 1 at a, got %v", a)
	}
	c, err := st.PopInt()
	if err != nil || c.Cmp(big.NewInt(3)) != 0 {
		t.Errorf("Expected 3 at c, got %v", c)
	}
	b, err := st.PopInt()
	if err != nil || b.Cmp(big.NewInt(2)) != 0 {
		t.Errorf("Expected 2 at b, got %v", b)
	}
}
