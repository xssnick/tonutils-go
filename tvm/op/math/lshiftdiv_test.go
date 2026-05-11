package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestLshiftdivOperation(t *testing.T) {
	tests := []struct {
		x, y, z int64
		want    int64
	}{
		{9, 2, 4, 72},
		{10, 160, 40, 68719476736},
		{5, 3, 40, 1832519379626},
		{5, 40, 3, 1},
		{1, 3, 0, 0},
		{-5634879008887978, 37867867, 2, -595214831},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> x: %d y: %d z: %d, arg -> q: %d", test.x, test.y, test.z, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))
			st.PushInt(big.NewInt(test.z))

			op := LSHIFTDIV()
			err := op.Interpret(&vm.State{Stack: st})
			if err != nil {
				t.Fatal("Failed LSHIFTDIV execution:", err.Error())
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
