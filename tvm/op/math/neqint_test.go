package math

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
	"testing"
)

func TestNeqintOperation(t *testing.T) {
	var tests = []struct {
		cons int8
		arg  int
		want bool
	}{
		{1, 1, false},
		{1, 123, true},
		{20, 20, false},
		{20, -40, true},
		{-100, -101, true},
		{-100, -100, false},
	}

	st := vm.NewStack()
	for _, test := range tests {
		name := fmt.Sprintf("case -> const: %d, arg: %d", test.cons, test.arg)
		t.Run(name, func(t *testing.T) {
			operation := NEQINT(test.cons)

			err := st.Push(big.NewInt(int64(test.arg)))
			if err != nil {
				t.Fatal("Failed 'arg' argument pushing:", err.Error())
			}

			err = operation.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed NEQINT interpretation:", err.Error())
			}

			got, err := st.Pop()
			if err != nil {
				t.Fatal("Failed 'got' argument popping:", err.Error())
			}
			gotTyped := got.(*big.Int).Sign() != 0
			if gotTyped != test.want {
				t.Errorf("got %t, want %t\n", gotTyped, test.want)
			}
		})
	}
}
