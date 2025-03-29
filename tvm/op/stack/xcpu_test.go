package stack

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func Test_XCPU(t *testing.T) {
	st := vm.NewStack()

	st.PushAny(1)
	st.PushAny(2)
	st.PushAny(3)
	st.PushAny(4)
	xcpu := XCPU(2, 3)

	err := xcpu.Interpret(&vm.State{
		Stack: st,
	})
	if err != nil {
		t.Fatal("Failed ROT interpretation:", err.Error())
	}
	a, err := st.PopAny()
	if err != nil || a != 1 {
		t.Errorf("Expected 1 at a, got %v", a)
	}
	b, err := st.PopAny()
	if err != nil || b != 2 {
		t.Errorf("Expected 2 at b, got %v", b)
	}
	c, err := st.PopAny()
	if err != nil || c != 3 {
		t.Errorf("Expected 3 at c, got %v", c)
	}
	d, err := st.PopAny()
	if err != nil || d != 4 {
		t.Errorf("Expected 4 at d, got %v", d)
	}
	a, err = st.PopAny()
	if err != nil || a != 1 {
		t.Errorf("Expected 1 at a, got %v", a)
	}
}
