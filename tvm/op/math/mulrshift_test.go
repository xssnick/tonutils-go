package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMulrshiftOperation(t *testing.T) {
	tests := []struct {
		x, y, z int64
		want    int64
	}{
		{9, 2, 4, 1},
		{-9, 2, 4, -2},
		{0, 2, 4, 0},
		{9, 2, 0, 18},
		{-5634879008887978, 2, 4, -704359876110998},
		{11, 2, 4, 1},
		{-11, 2, 4, -2},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d z: %d, arg -> q: %d", test.x, test.y, test.z, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))
			st.PushInt(big.NewInt(test.z))

			op := MULRSHIFT()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed MULRSHIFT execution:", err.Error())
			}

			quotient, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop quotient:", err.Error())
			}

			if quotient.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Expected quotient %d, got %d", test.want, quotient)
			}
		})
	}
}
