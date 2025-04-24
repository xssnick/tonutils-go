package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestLshiftmodOperation(t *testing.T) {
	tests := []struct {
		x, y, z int64
		want    int64
	}{
		{9, 2, 4, 0},
		{10, 160, 40, 0},
		{5, 3, 40, 2},
		{5, 40, 3, 0},
		{1, 3, 0, 1},
		{-5634879008887978, 37867867, 2, 21183565},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d z: %d, arg -> r: %d", test.x, test.y, test.z, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))
			st.PushInt(big.NewInt(test.z))

			op := LSHIFTMOD()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed LSHIFTMOD execution:", err.Error())
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
