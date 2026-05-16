package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestAddDivModcOperation(t *testing.T) {
	tests := []struct {
		x, w, z int64
		q, r    int64
	}{
		{10, 3, 4, 4, -3},
		{12, 3, 3, 5, 0},
		{1, 9, 4, 3, -2},
		{10, -3, 4, 2, -1},
		{-10, 3, 4, -1, -3},
		{10, 3, -4, -3, 1},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d w: %d z: %d, arg -> q: %d r: %d", test.x, test.w, test.z, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.w))
			st.PushInt(big.NewInt(test.z))

			adddivmodc := ADDDIVMODC()
			err := adddivmodc.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed ADDDIVMODC execution:", err.Error())
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
