package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMulmodpow2rcodeOperation(t *testing.T) {
	tests := []struct {
		code []byte
		x, y int64
		want int64
	}{
		{[]byte{0xA9, 0xB9, 0x03}, 9, 2, 2},
		{[]byte{0xA9, 0xB9, 0x03}, -9, 2, -2},
		{[]byte{0xA9, 0xB9, 0x03}, 0, 2, 0},
		{[]byte{0xA9, 0xB9, 0x03}, -5634879008887978, 2, -4},
		{[]byte{0xA9, 0xB9, 0x03}, 11, 2, 6},
		{[]byte{0xA9, 0xB9, 0x03}, -11, 2, -6},
		{[]byte{0xA9, 0xB9, 0x03}, 20, 2, -8},
		{[]byte{0xA9, 0xB9, 0x03}, -20, 2, -8},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> code: %x x: %d y: %d arg: r: %d", test.code, test.x, test.y, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))

			codeCell := cell.BeginCell().MustStoreBinarySnake(test.code).EndCell()
			codeSlice := codeCell.BeginParse()

			op := MULMODPOW2RCODE(0)
			op.Deserialize(codeSlice)

			err := op.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal("Failed MULMODPOW2R# execution:", err.Error())
			}

			remainder, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop remainder: ", err.Error())
			}

			if remainder.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Expected remainder %d, got %d", test.want, remainder)
			}
		})
	}
}
