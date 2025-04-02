package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func Test_REVERSE(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		st := vm.NewStack()

		st.PushAny(big.NewInt(1))
		st.PushAny(big.NewInt(2))
		st.PushAny(big.NewInt(3))
		st.PushAny(big.NewInt(4))
		blkpush := REVERSE(1, 0)

		err := blkpush.Interpret(&vm.State{
			Stack: st,
		})
		if err != nil {
			t.Fatal("Failed ROT interpretation:", err.Error())
		}

		a, err := st.PopInt()
		if err != nil || a.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		b, err := st.PopInt()
		if err != nil || b.Cmp(big.NewInt(2)) != 0 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		c, err := st.PopInt()
		if err != nil || c.Cmp(big.NewInt(3)) != 0 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
		d, err := st.PopInt()
		if err != nil || d.Cmp(big.NewInt(4)) != 0 {
			t.Errorf("Expected 4 at d, got %v", d)
		}
	})
	t.Run("part", func(t *testing.T) {
		st := vm.NewStack()

		st.PushAny(big.NewInt(1))
		st.PushAny(big.NewInt(2))
		st.PushAny(big.NewInt(3))
		st.PushAny(big.NewInt(4))
		blkpush := REVERSE(0, 1)

		err := blkpush.Interpret(&vm.State{
			Stack: st,
		})
		if err != nil {
			t.Fatal("Failed ROT interpretation:", err.Error())
		}

		d, err := st.PopInt()
		if err != nil || d.Cmp(big.NewInt(4)) != 0 {
			t.Errorf("Expected 4 at d, got %v", d)
		}
		b, err := st.PopInt()
		if err != nil || b.Cmp(big.NewInt(2)) != 0 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		c, err := st.PopInt()
		if err != nil || c.Cmp(big.NewInt(3)) != 0 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
		a, err := st.PopInt()
		if err != nil || a.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
	})

}
