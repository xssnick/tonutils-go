package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMulConstOperation(t *testing.T) {
	tests := []struct {
		code []byte
		x    int64
		want int64
	}{
		{[]byte{0xA7, 0x0A}, 5, 50},
		{[]byte{0xA7, 0xFB}, 0, 0},
		{[]byte{0xA7, 0xFB}, 5, -25},
		{[]byte{0xA7, 0xFB}, -5, 25},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> code: %x cc: %d arg: %d", test.code, test.x, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))

			codeCell := cell.BeginCell().MustStoreBinarySnake(test.code).EndCell()
			codeSlice := codeCell.BeginParse()

			op := MULCONST(0)

			op.Deserialize(codeSlice)

			err := op.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal(err)
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed MULCONST pop: ", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("got %d, want %d\n", res, test.want)
			}
		})
	}
}
