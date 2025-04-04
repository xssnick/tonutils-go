package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMulAddDivModc(t *testing.T) {
	tests := []struct {
		x, y, w, z int64
		q, r       int64
	}{
		{9, 2, 4, 3, 8, -2},
		{-9, 2, 4, 3, -4, -2},
		{-9, 2, 4, -3, 5, 1},
		{0, 2, 4, 3, 2, -2},
		{9, 2, 0, 3, 6, 0},
		{-5634879008887978, 2, 4, 3, -3756586005925317, -1},
		{11, 2, 4, 3, 9, -1},
		{-11, 2, 4, 3, -6, 0},
		{11, 2, -4, 3, 6, 0},
		{11, 2, 4, -3, -8, 2},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d w: %d z: %d, arg -> q: %d r: %d", test.x, test.y, test.w, test.z, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))
			st.PushInt(big.NewInt(test.w))
			st.PushInt(big.NewInt(test.z))

			op := MULADDDIVMODC()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed MULADDDIVMODC execution:", err.Error())
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
