package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestDecOperation(t *testing.T) {
	tests := []struct {
		input int
		want  int64
	}{
		{10, 9},
		{0, -1},
		{-5, -6},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("DEC (%d) -> %d", test.input, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(int64(test.input)))

			dec := DEC()

			err := dec.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal("Failed DEC interpretation:", err.Error())
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed DEC pop: ", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Want %d, got %d", test.want, res.Int64())
			}
		})
	}
}
