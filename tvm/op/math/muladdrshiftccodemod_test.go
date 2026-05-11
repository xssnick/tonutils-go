package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMuladdrshiftccodemodOperation(t *testing.T) {
	tests := []struct {
		code    []byte
		x, y, w int64
		q, r    int64
	}{
		{[]byte{0xA9, 0xB2, 0x02}, 9, 2, 4, 3, -2},
		{[]byte{0xA9, 0xB2, 0x02}, -9, 2, 4, -1, -6},
		{[]byte{0xA9, 0xB2, 0x02}, -9, 2, -4, -2, -6},
		{[]byte{0xA9, 0xB2, 0x02}, 0, 2, 4, 1, -4},
		{[]byte{0xA9, 0xB2, 0x02}, 9, 2, 0, 3, -6},
		{[]byte{0xA9, 0xB2, 0x02}, -5634879008887978, 2, 4, -1408719752221994, 0},
		{[]byte{0xA9, 0xB2, 0x02}, 11, 2, 4, 4, -6},
		{[]byte{0xA9, 0xB2, 0x02}, -11, 2, 4, -2, -2},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> code: %x x: %d y: %d w: %d arg: q: %d r: %d", test.code, test.x, test.y, test.w, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))
			st.PushInt(big.NewInt(test.y))
			st.PushInt(big.NewInt(test.w))

			codeCell := cell.BeginCell().MustStoreBinarySnake(test.code).EndCell()
			codeSlice := codeCell.BeginParse()

			op := MULADDRSHIFTCCODEMOD(0)
			op.Deserialize(codeSlice)

			err := op.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal("Failed MULADDRSHIFTC#MOD execution:", err.Error())
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
