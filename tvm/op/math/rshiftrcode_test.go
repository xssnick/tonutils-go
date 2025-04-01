package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestRshiftrcodeOperation(t *testing.T) {
	tests := []struct {
		code []byte
		x    int64
		want int64
	}{
		{[]byte{0xA9, 0x35, 0x02}, 10, 1},
		{[]byte{0xA9, 0x35, 0x03}, 10, 1},
		{[]byte{0xA9, 0x35, 0x02}, 5, 1},
		{[]byte{0xA9, 0x35, 0x02}, -7, -1},
		{[]byte{0xA9, 0x35, 0x03}, 0, 0},
		{[]byte{0xA9, 0x35, 0x01}, -5634879008887978, -1408719752221994},
		{[]byte{0xA9, 0x35, 0x01}, -7, -2},
		{[]byte{0xA9, 0x35, 0x01}, -13, -3},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> code: %x x: %d arg: %d", test.code, test.x, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))

			codeCell := cell.BeginCell().MustStoreBinarySnake(test.code).EndCell()
			codeSlice := codeCell.BeginParse()

			op := RSHIFTRCODE(0)
			op.Deserialize(codeSlice)

			err := op.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal(err)
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed RSHIFTR# pop: ", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("got %d, want %d\n", res, test.want)
			}
		})
	}
}
