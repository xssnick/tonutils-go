package math

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
	"testing"
)

func TestIszeroOperation(t *testing.T) {
	var tests = []struct {
		a    int
		want bool
	}{
		{0, true},
		{-42, false},
		{-65, false},
		{4, false},
		{-1248, false},
		{345, false},
	}

	st := vm.NewStack()
	for _, test := range tests {
		name := fmt.Sprintf("case %d", test.a)
		t.Run(name, func(t *testing.T) {
			operation := ISZERO()
			arg := test.a

			err := st.Push(big.NewInt(int64(arg)))
			if err != nil {
				t.Fatal("Failed argument pushing:", err.Error())
			}

			err = operation.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed ABS interpretation:", err.Error())
			}

			got, err := st.Pop()
			if err != nil {
				t.Fatal("Failed result popping:", err.Error())
			}
			gotTyped := got.(*big.Int).Sign() != 0
			if gotTyped != test.want {
				t.Errorf("got %t, want %t\n", got, test.want)
			}
		})
	}
}
