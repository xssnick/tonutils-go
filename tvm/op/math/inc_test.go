package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestIncOperation(t *testing.T) {
	tests := []struct {
		input int
		want  int64
	}{
		{1, 2},
		{-1, 0},
		{100, 101},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("INC(%d) -> %d", test.input, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(int64(test.input)))

			inc := INC()

			err := inc.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal("Failed INC interpretation:", err.Error())
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed INC pop: ", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Want %d, got %d", test.want, res.Int64())
			}
		})
	}
}
