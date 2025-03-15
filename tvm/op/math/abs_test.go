package math

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
	"testing"
)

func TestAbsOperation(t *testing.T) {
	var tests = []struct {
		a    int
		want int64
	}{
		{1, 1},
		{-42, 42},
		{-65, 65},
		{0, 0},
		{-1248, 1248},
		{345, 345},
	}

	st := vm.NewStack()
	for _, test := range tests {
		name := fmt.Sprintf("case %d", test.a)
		t.Run(name, func(t *testing.T) {
			operation := ABS()
			arg := test.a

			err := st.Push(big.NewInt(int64(arg)))
			if err != nil {
				t.Fatal("Failed argument pushing:", err.Error())
			}

			err = operation.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal("Failed ABS interpretation:", err.Error())
			}

			got, err := st.Pop()
			if err != nil {
				t.Fatal("Failed ABS pop:", err.Error())
			}
			gotTyped := got.(*big.Int)
			wantTyped := big.NewInt(test.want)
			if wantTyped.Cmp(gotTyped) != 0 {
				t.Errorf("got %s, want %d\n", got, test.want)
			}
		})
	}
}
