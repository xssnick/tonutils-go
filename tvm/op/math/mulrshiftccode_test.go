package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMulrshiftccodeOperation(t *testing.T) {
	tests := []struct {
		code []byte
		x, y int64
		want int64
	}{
		{[]byte{0xA9, 0xB6, 0x03}, 9, 2, 2},
		{[]byte{0xA9, 0xB6, 0x03}, -9, 2, -1},
		{[]byte{0xA9, 0xB6, 0x03}, 0, 2, 0},
		{[]byte{0xA9, 0xB6, 0x03}, -5634879008887978, 2, -704359876110997},
		{[]byte{0xA9, 0xB6, 0x03}, 11, 2, 2},
		{[]byte{0xA9, 0xB6, 0x03}, -11, 2, -1},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> code: %x x: %d y: %d arg: q: %d", test.code, test.x, test.y, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))

			codeCell := cell.BeginCell().MustStoreBinarySnake(test.code).EndCell()
			codeSlice := codeCell.BeginParse()

			op := MULRSHIFTCCODE(0)
			op.Deserialize(codeSlice)

			err := op.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal("Failed MULRSHIFTC# execution:", err.Error())
			}

			quotient, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop quotient: ", err.Error())
			}

			if quotient.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("Expected quotient %d, got %d", test.want, quotient)
			}
		})
	}
}
