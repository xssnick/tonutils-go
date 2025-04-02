package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func Test_BLKDROP2(t *testing.T) {
	st := vm.NewStack()

	st.PushAny(big.NewInt(1))
	st.PushAny(big.NewInt(2))
	st.PushAny(big.NewInt(3))
	st.PushAny(big.NewInt(4))
	blkdrop2 := BLKDROP2(2, 1)

	err := blkdrop2.Interpret(&vm.State{
		Stack: st,
	})
	if err != nil {
		t.Fatal("Failed ROT interpretation:", err.Error())
	}

	d, err := st.PopInt()
	if err != nil || d.Cmp(big.NewInt(4)) != 0 {
		t.Errorf("Expected 4 at d, got %v", d)
	}
	c, err := st.PopInt()
	if err != nil || c.Cmp(big.NewInt(3)) != 0 {
		t.Errorf("Expected 3 at c, got %v", c)
	}
}
