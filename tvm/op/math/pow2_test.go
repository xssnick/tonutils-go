package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestPow2Operation(t *testing.T) {
	tests := []struct {
		y    int64
		want int64
	}{
		{10, 1024},
		{4, 16},
		{5, 32},
		{0, 1},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> y: %d, arg -> %d", test.y, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.y))

			op := POW2()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed POW2 execution:", err.Error())
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
