package math

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
	"testing"
)

func TestIsposOperation(t *testing.T) {
	var tests = []struct {
		arg  int
		want bool
	}{
		{1, true},
		{123, true},
		{0, false},
		{-40, false},
		{-101, false},
		{-100, false},
	}

	st := vm.NewStack()
	for _, test := range tests {
		name := fmt.Sprintf("case -> arg: %d", test.arg)
		t.Run(name, func(t *testing.T) {
			operation := ISPOS()

			err := st.Push(big.NewInt(int64(test.arg)))
			if err != nil {
				t.Fatal("Failed 'arg' argument pushing:", err.Error())
			}

			err = operation.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed ISPOS interpretation:", err.Error())
			}

			got, err := st.Pop()
			if err != nil {
				t.Fatal("Failed ISPOS result popping:", err.Error())
			}
			gotTyped := got.(*big.Int).Sign() != 0
			if gotTyped != test.want {
				t.Errorf("got %t, want %t\n", gotTyped, test.want)
			}
		})
	}
}
