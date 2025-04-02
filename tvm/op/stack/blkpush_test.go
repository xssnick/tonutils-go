package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func Test_BLKPUSH(t *testing.T) {
	st := vm.NewStack()

	st.PushAny(big.NewInt(1))
	st.PushAny(big.NewInt(2))
	st.PushAny(big.NewInt(3))
	st.PushAny(big.NewInt(4))
	blkpush := BLKPUSH(2, 2)

	err := blkpush.Interpret(&vm.State{
		Stack: st,
	})
	if err != nil {
		t.Fatal("Failed ROT interpretation:", err.Error())
	}

	c, err := st.PopInt()
	if err != nil || c.Cmp(big.NewInt(3)) != 0 {
		t.Errorf("Expected 3 at c, got %v", c)
	}
	b, err := st.PopInt()
	if err != nil || b.Cmp(big.NewInt(2)) != 0 {
		t.Errorf("Expected 2 at b, got %v", b)
	}
	d, err := st.PopInt()
	if err != nil || d.Cmp(big.NewInt(4)) != 0 {
		t.Errorf("Expected 4 at d, got %v", d)
	}
	c, err = st.PopInt()
	if err != nil || c.Cmp(big.NewInt(3)) != 0 {
		t.Errorf("Expected 3 at c, got %v", c)
	}
	b, err = st.PopInt()
	if err != nil || b.Cmp(big.NewInt(2)) != 0 {
		t.Errorf("Expected 2 at b, got %v", b)
	}
	a, err := st.PopInt()
	if err != nil || a.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("Expected 1 at a, got %v", a)
	}

}
