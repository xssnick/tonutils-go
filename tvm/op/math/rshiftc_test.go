package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestRshiftcOperation(t *testing.T) {
	tests := []struct {
		x, y int64
		want int64
	}{
		{10, 3, 2},
		{10, 4, 1},
		{5, 3, 1},
		{-7, 3, 0},
		{0, 3, 0},
		{-5634879008887978, 34, -327993},
		{-5634879008887978, 3, -704359876110997},
		{-7, 2, -1},
		{-13, 2, -3},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d, arg -> %d", test.x, test.y, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))

			op := RSHIFTC()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed RSHIFTC execution:", err.Error())
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop result:", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Expected result %d, got %d", test.want, res)
			}
		})
	}
}
