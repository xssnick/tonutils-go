package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestAddrshiftmodrOperation(t *testing.T) {
	tests := []struct {
		x, w, z int64
		q, r    int64
	}{
		{9, 2, 4, 1, -5},
		{-9, 2, 4, 0, -7},
		{0, 2, 4, 0, 2},
		{9, 2, 0, 11, 0},
		{-5634879008887978, 2, 4, -352179938055498, -8},
		{11, 2, 4, 1, -3},
		{-11, 2, 4, -1, 7},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d w: %d z: %d, arg -> q: %d r: %d", test.x, test.w, test.z, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.w))
			st.PushInt(big.NewInt(test.z))

			op := ADDRSHIFTMODR()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed ADDRSHIFTMODR execution:", err.Error())
			}

			remainder, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop remainder:", err.Error())
			}

			if remainder.Cmp(big.NewInt(test.r)) != 0 {
				t.Errorf("Expected remainder %d, got %d", test.r, remainder)
			}

			quotient, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop quotient:", err.Error())
			}

			if quotient.Cmp(big.NewInt(test.q)) != 0 {
				t.Errorf("Expected quotient %d, got %d", test.q, quotient)
			}
		})
	}
}
