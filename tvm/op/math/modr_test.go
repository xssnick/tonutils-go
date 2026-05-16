package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestModrOperation(t *testing.T) {
	tests := []struct {
		x, y int64
		want int64
	}{
		{10, 3, 1},
		{10, 4, -2},
		{5, 2, -1},
		{-7, 3, -1},
		{7, -3, 1},
		{-7, -3, -1},
		{0, -3, 0},
		{-5, -2, 1},
		{-5634879008887978, 34534534534, -17147113334},
		{7, -2, 1},
		{-7, 2, -1},
		{-13, 2, -1},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d, arg -> r: %d", test.x, test.y, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))

			op := MODR()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed MODR execution:", err.Error())
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop remainder:", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Expected remainder %d, got %d", test.want, res)
			}
		})
	}
}
