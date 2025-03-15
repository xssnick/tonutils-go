package stack

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestRotOperation(t *testing.T) {
	st := vm.NewStack()

	st.Push(1)
	st.Push(2)
	st.Push(3)

	rot := ROT()

	err := rot.Interpret(&vm.State{
		Stack: st,
	})
	if err != nil {
		t.Fatal("Failed ROT interpretation:", err.Error())
	}

	a, err := st.Pop()
	if err != nil || a != 1 {
		t.Errorf("Expected 1 at a, got %v", a)
	}

	c, err := st.Pop()
	if err != nil || c != 3 {
		t.Errorf("Expected 3 at c, got %v", c)
	}

	b, err := st.Pop()
	if err != nil || b != 2 {
		t.Errorf("Expected 2 at b, got %v", b)
	}
}
