package math

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
	"testing"
)

func TestEqintOperation(t *testing.T) {
	var tests = []struct {
		cons int8
		arg  int
		want bool
	}{
		{1, 1, true},
		{1, 123, false},
		{20, 20, true},
		{20, -40, false},
		{-100, -101, false},
		{-100, -100, true},
	}

	st := vm.NewStack()
	for _, test := range tests {
		name := fmt.Sprintf("case -> const: %d, arg: %d", test.cons, test.arg)
		t.Run(name, func(t *testing.T) {
			operation := EQINT(test.cons)

			err := st.PushInt(big.NewInt(int64(test.arg)))
			if err != nil {
				t.Fatal("Failed 'arg' argument pushing:", err.Error())
			}

			err = operation.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed EQINT interpretation:", err.Error())
			}

			got, err := st.PopAny()
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
