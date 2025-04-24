package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMulrshiftcodemodOperation(t *testing.T) {
	tests := []struct {
		code []byte
		x, y int64
		q, r int64
	}{
		{[]byte{0xA9, 0xBC, 0x03}, 9, 2, 1, 2},
		{[]byte{0xA9, 0xBC, 0x03}, -9, 2, -2, 14},
		{[]byte{0xA9, 0xBC, 0x03}, 0, 2, 0, 0},
		{[]byte{0xA9, 0xBC, 0x03}, -5634879008887978, 2, -704359876110998, 12},
		{[]byte{0xA9, 0xBC, 0x03}, 11, 2, 1, 6},
		{[]byte{0xA9, 0xBC, 0x03}, -11, 2, -2, 10},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> code: %x x: %d y: %d arg: q: %d r: %d", test.code, test.x, test.y, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))

			codeCell := cell.BeginCell().MustStoreBinarySnake(test.code).EndCell()
			codeSlice := codeCell.BeginParse()

			op := MULRSHIFTCODEMOD(0)
			op.Deserialize(codeSlice)

			err := op.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal("Failed MULRSHIFT#MOD execution:", err.Error())
			}

			remainder, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop remainder: ", err.Error())
			}

			if remainder.Cmp(big.NewInt(test.r)) != 0 {
				t.Errorf("Expected remainder %d, got %d", test.r, remainder)
			}

			quotient, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop quotient: ", err.Error())
			}

			if quotient.Cmp(big.NewInt(test.q)) != 0 {
				t.Errorf("Expected quotient %d, got %d", test.q, quotient)
			}
		})
	}
}
