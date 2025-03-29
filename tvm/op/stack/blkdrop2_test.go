package stack

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func Test_BLKDROP2(t *testing.T) {
	st := vm.NewStack()

	st.PushAny(1)
	st.PushAny(2)
	st.PushAny(3)
	st.PushAny(4)
	blkdrop2 := BLKDROP2(2, 1)

	err := blkdrop2.Interpret(&vm.State{
		Stack: st,
	})
	if err != nil {
		t.Fatal("Failed ROT interpretation:", err.Error())
	}

	d, err := st.PopAny()
	if err != nil || d != 4 {
		t.Errorf("Expected 4 at d, got %v", d)
	}

	c, err := st.PopAny()
	if err != nil || c != 3 {
		t.Errorf("Expected 3 at c, got %v", c)
	}

}
