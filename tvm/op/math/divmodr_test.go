package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestDivmodrOperation(t *testing.T) {
	tests := []struct {
		x, y int64
		q, r int64
	}{
		{10, -3, -3, 1},
		{10, 3, 3, 1},
		{-10, -3, 3, -1},
		{-10, 3, -3, -1},
		{10, 4, 3, -2},
		{10, -4, -2, 2},
		{-10, 4, -2, -2},
		{-5634879008887978, 34534534534, -163166, -17147113334},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d, arg -> q: %d r: %d", test.x, test.y, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))

			op := DIVMODR()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed DIVMODR execution:", err.Error())
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
