package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestDivOperation(t *testing.T) {
	tests := []struct {
		x, y int64
		want int64
	}{
		{10, 3, 3},
		{10, -3, -4},
		{-10, 3, -4},
		{-10, -3, 3},
		{5, -3, -2},
		{-5, 3, -2},
		{0, -3, 0},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d, arg -> q: %d", test.x, test.y, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))

			op := DIV()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed DIV execution:", err.Error())
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop quotient:", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Expected quotient %d, got %d", test.want, res)
			}
		})
	}
}

func TestDivOperationDivisionByZero(t *testing.T) {
	st := vm.NewStack()
	st.PushInt(big.NewInt(10))
	st.PushInt(big.NewInt(0))

	op := DIV()
	if err := op.Interpret(&vm.State{Stack: st}); err == nil {
		t.Fatal("expected division by zero error")
	}
}
