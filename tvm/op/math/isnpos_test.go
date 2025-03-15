package math

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
	"testing"
)

func TestIsnposOperation(t *testing.T) {
	tests := []struct {
		arg  int
		want bool
	}{
		{0, true},
		{-17, true},
		{-100, true},
		{1, false},
		{14, false},
		{140, false},
	}

	st := vm.NewStack()
	for _, test := range tests {
		name := fmt.Sprintf("case -> arg: %d ", test.arg)
		t.Run(name, func(t *testing.T) {
			operation := ISNPOS()

			err := st.Push(big.NewInt(int64(test.arg)))
			if err != nil {
				t.Fatal("Failed 'arg' argument pushing:", err.Error())
			}

			err = operation.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed ISNPOS interpretation:", err.Error())
			}

			got, err := st.Pop()
			if err != nil {
				t.Fatal("Failed ISNPOS result popping:", err.Error())
			}
			gotTyped := got.(*big.Int).Sign() != 0
			if gotTyped != test.want {
				t.Errorf("got %t, want %t\n", gotTyped, test.want)
			}
		})
	}

}
