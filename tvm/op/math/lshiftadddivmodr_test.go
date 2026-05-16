package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestLshiftadddivmodrOperation(t *testing.T) {
	tests := []struct {
		x, w, z, y int64
		q, r       int64
	}{
		{9, 2, 4, 3, 19, -2},
		{-9, 2, 4, 3, -17, -2},
		{-5634879008887978, 2, 4, 3, -11269758017775955, -2},
		{11, 2, 4, 3, 23, -2},
		{-11, 2, 4, 3, -21, -2},
		{11, 2, -4, 3, -22, 2},
		{-11, 2, -4, 3, 22, 2},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d w: %d z: %d y: %d, arg -> q: %d r: %d", test.x, test.w, test.z, test.y, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.w))
			st.PushInt(big.NewInt(test.z))
			st.PushInt(big.NewInt(test.y))

			op := LSHIFTADDDIVMODR()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed LSHIFTADDDIVMODR execution:", err.Error())
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
