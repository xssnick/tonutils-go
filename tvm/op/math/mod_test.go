package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestModOperation(t *testing.T) {
	tests := []struct {
		x, y int64
		want int64
	}{
		{10, 3, 1},
		{10, -3, -2},
		{-10, 3, 2},
		{-10, -3, -1},
		{7, -2, -1},
		{-7, 4, 1},
		{0, -3, 0},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d, arg -> r: %d", test.x, test.y, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))

			op := MOD()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed MOD execution:", err.Error())
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop remainder:", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Expected remainder %d, got %d", test.want, res)
			}

			if test.y < 0 {
				if res.Sign() > 0 || res.Cmp(big.NewInt(test.y)) <= 0 {
					t.Errorf("Expected remainder to satisfy 0 >= r > %d, got %d", test.y, res)
				}
			} else {
				if res.Sign() < 0 || res.Cmp(big.NewInt(test.y)) >= 0 {
					t.Errorf("Expected remainder to satisfy 0 <= r < %d, got %d", test.y, res)
				}
			}
		})
	}
}

func TestModOperationDivisionByZero(t *testing.T) {
	st := vm.NewStack()
	st.PushInt(big.NewInt(10))
	st.PushInt(big.NewInt(0))

	op := MOD()
	if err := op.Interpret(&vm.State{Stack: st}); err == nil {
		t.Fatal("expected division by zero error")
	}
}
