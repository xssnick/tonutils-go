package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestSubOperation(t *testing.T) {
	tests := []struct {
		x    int
		y    int
		want int64
	}{
		{10, 3, 7},
		{10, 10, 0},
		{3, 10, -7},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> const: %d - %d arg: %d", test.x, test.y, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(int64(test.x)))
			st.PushInt(big.NewInt(int64(test.y)))

			sub := SUB()

			err := sub.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal("Failed SUB interpretation:", err.Error())
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed SUB pop: ", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Want %d, got %d", test.want, res.Int64())
			}
		})
	}
}
