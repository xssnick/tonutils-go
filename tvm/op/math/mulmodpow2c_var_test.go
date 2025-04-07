package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMulmodpow2c_varOperation(t *testing.T) {
	tests := []struct {
		x, y, z int64
		want    int64
	}{
		{9, 2, 4, -14},
		{-9, 2, 4, -2},
		{0, 2, 4, 0},
		{9, 2, 0, 0},
		{x: -5634879008887978, y: 2, z: 4, want: -4},
		{11, 2, 4, -10},
		{-11, 2, 4, -6},
		{20, 2, 4, -8},
		{-20, 2, 4, -8},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d z: %d, arg -> r: %d", test.x, test.y, test.z, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))
			st.PushInt(big.NewInt(test.z))

			op := MULMODPOW2C_VAR()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed MULMODPOW2C_VAR execution:", err.Error())
			}

			remainder, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop remainder:", err.Error())
			}

			if remainder.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Expected remainder %d, got %d", test.want, remainder)
			}
		})
	}
}
