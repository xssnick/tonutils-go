package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestModpow2ccodeOperation(t *testing.T) {
	tests := []struct {
		code []byte
		x    int64
		want int64
	}{
		{[]byte{0xA9, 0x3A, 0x02}, 10, -6},
		{[]byte{0xA9, 0x3A, 0x03}, 10, -6},
		{[]byte{0xA9, 0x3A, 0x02}, 5, -3},
		{[]byte{0xA9, 0x3A, 0x02}, -7, -7},
		{[]byte{0xA9, 0x3A, 0x03}, 0, 0},
		{[]byte{0xA9, 0x3A, 0x21}, -5634879008887978, -2175620266},
		{[]byte{0xA9, 0x3A, 0x02}, -5634879008887978, -2},
		{[]byte{0xA9, 0x3A, 0x01}, -7, -3},
		{[]byte{0xA9, 0x3A, 0x01}, -13, -1},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> code: %x x: %d arg: %d", test.code, test.x, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))

			codeCell := cell.BeginCell().MustStoreBinarySnake(test.code).EndCell()
			codeSlice := codeCell.BeginParse()

			op := MODPOW2CCODE(0)
			op.Deserialize(codeSlice)

			err := op.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal(err)
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed MODPOW2C# pop: ", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("got %d, want %d\n", res, test.want)
			}
		})
	}
}
