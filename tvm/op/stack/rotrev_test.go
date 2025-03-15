package stack

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestRotRevOperation(t *testing.T) {
	st := vm.NewStack()

	st.Push(1)
	st.Push(2)
	st.Push(3)

	rot := ROTREV()

	err := rot.Interpret(&vm.State{
		Stack: st,
	})
	if err != nil {
		t.Fatal("Failed ROT interpretation:", err.Error())
	}

	b, err := st.Pop()
	if err != nil || b != 2 {
		t.Errorf("Expected 3 at b, got %v", b)
	}

	a, err := st.Pop()
	if err != nil || a != 1 {
		t.Errorf("Expected 1 at a, got %v", a)
	}

	c, err := st.Pop()
	if err != nil || c != 3 {
		t.Errorf("Expected 2 at c, got %v", c)
	}
}
