package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMulrshiftcmod_varOperation(t *testing.T) {
	tests := []struct {
		x, y, z int64
		q, r    int64
	}{
		{9, 2, 4, 2, -14},
		{-9, 2, 4, -1, -2},
		{0, 2, 4, 0, 0},
		{9, 2, 0, 18, 0},
		{-5634879008887978, 2, 4, -704359876110997, -4},
		{11, 2, 4, 2, -10},
		{-11, 2, 4, -1, -6},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d z: %d, arg -> q: %d r: %d", test.x, test.y, test.z, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))
			st.PushInt(big.NewInt(test.z))

			op := MULRSHIFTCMOD_VAR()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed MULRSHIFTCMOD_VAR execution:", err.Error())
			}

			remainder, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop remainder:", err.Error())
			}

			if remainder.Cmp(big.NewInt(test.r)) != 0 {
				t.Errorf("Expected remainder %d, got %d", test.r, remainder)
			}

			quotient, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop quotient:", err.Error())
			}

			if quotient.Cmp(big.NewInt(test.q)) != 0 {
				t.Errorf("Expected quotient %d, got %d", test.q, quotient)
			}
		})
	}
}
