package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestRshiftmodOperation(t *testing.T) {
	tests := []struct {
		x, y int64
		q, r int64
	}{
		{10, 3, 1, 2},
		{10, 4, 0, 10},
		{5, 3, 0, 5},
		{-7, 3, -1, 1},
		{0, 3, 0, 0},
		{-5634879008887978, 34, -327994, 15004248918},
		{-5634879008887978, 2, -1408719752221995, 2},
		{-7, 2, -2, 1},
		{-13, 2, -4, 3},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d, arg -> q: %d r: %d", test.x, test.y, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))

			op := RSHIFTMOD()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed RSHIFTMOD execution:", err.Error())
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
