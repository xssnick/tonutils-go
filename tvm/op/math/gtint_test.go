package math

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/int257"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"testing"
)

func TestGtintOperation(t *testing.T) {
	var tests = []struct {
		cons int8
		arg  int
		want bool
	}{
		{1, 0, false},
		{0, 2, true},
		{1, 1, false},
		{20, -19, false},
		{-100, -101, false},
		{-100, 200, true},
	}

	st := vm.NewStack()
	for _, test := range tests {
		name := fmt.Sprintf("case -> const: %d, arg: %d", test.cons, test.arg)
		t.Run(name, func(t *testing.T) {
			operation := GTINT(test.cons)

			err := st.Push(int257.NewInt257FromInt64(int64(test.arg)))
			if err != nil {
				t.Fatal("Failed 'arg' argument pushing:", err.Error())
			}

			err = operation.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal("Failed GTINT interpretation:", err.Error())
			}

			got, err := st.Pop()
			if err != nil {
				t.Fatal("Failed GTINT result popping:", err.Error())
			}
			gotTyped := got.(int257.Int257).Bool()
			if gotTyped != test.want {
				t.Errorf("got %t, want %t\n", gotTyped, test.want)
			}
		})
	}
}
