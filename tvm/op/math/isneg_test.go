package math

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
	"testing"
)

func TestIsnegOperation(t *testing.T) {
	var tests = []struct {
		arg  int
		want bool
	}{
		{1, false},
		{123, false},
		{0, false},
		{-40, true},
		{-101, true},
		{-100, true},
	}

	st := vm.NewStack()
	for _, test := range tests {
		name := fmt.Sprintf("case -> arg: %d", test.arg)
		t.Run(name, func(t *testing.T) {
			operation := ISNEG()

			err := st.PushInt(big.NewInt(int64(test.arg)))
			if err != nil {
				t.Fatal("Failed 'arg' argument pushing:", err.Error())
			}

			err = operation.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed ISNEG interpretation:", err.Error())
			}

			got, err := st.PopAny()
			if err != nil {
				t.Fatal("Failed ISNEG result popping:", err.Error())
			}
			gotTyped := got.(*big.Int).Sign() != 0
			if gotTyped != test.want {
				t.Errorf("got %t, want %t\n", gotTyped, test.want)
			}
		})
	}
}
