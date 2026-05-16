package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMuldivr(t *testing.T) {
	tests := []struct {
		x, y, z int64
		want    int64
	}{
		{9, 2, 4, 5},
		{-9, 2, 4, -4},
		{0, 2, 4, 0},
		{-5634879008887978, 2, 4, -2817439504443989},
		{19, 5, 4, 24},
		{-19, 5, 4, -24},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d z: %d, arg -> q: %d", test.x, test.y, test.z, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))
			st.PushInt(big.NewInt(test.z))

			op := MULDIVR()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed MULDIVR execution:", err.Error())
			}

			quotient, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop quotient:", err.Error())
			}

			if quotient.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Expected quotient %d, got %d", test.want, quotient)
			}
		})
	}
}
