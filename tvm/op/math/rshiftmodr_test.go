package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestRshiftmodrOperation(t *testing.T) {
	tests := []struct {
		x, y int64
		q, r int64
	}{
		{10, 3, 1, 2},
		{10, 4, 1, -6},
		{5, 3, 1, -3},
		{-7, 3, -1, 1},
		{0, 3, 0, 0},
		{-5634879008887978, 34, -327993, -2175620266},
		{-5634879008887978, 2, -1408719752221994, -2},
		{-7, 2, -2, 1},
		{-13, 2, -3, -1},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d, arg -> q: %d r: %d", test.x, test.y, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))

			op := RSHIFTMODR()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed RSHIFTMODR execution:", err.Error())
			}

			r, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop remainder:", err.Error())
			}

			if r.Cmp(big.NewInt(test.r)) != 0 {
				t.Errorf("Expected remainder %d, got %d", test.r, r)
			}

			q, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop quotient:", err.Error())
			}

			if q.Cmp(big.NewInt(test.q)) != 0 {
				t.Errorf("Expected quotient %d, got %d", test.q, q)
			}
		})
	}
}
