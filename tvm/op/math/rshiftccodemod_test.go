package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestRshiftccodemodOperation(t *testing.T) {
	tests := []struct {
		code []byte
		x    int64
		q, r int64
	}{
		{[]byte{0xA9, 0x3E, 0x02}, 10, 2, -6},
		{[]byte{0xA9, 0x3E, 0x03}, 10, 1, -6},
		{[]byte{0xA9, 0x3E, 0x02}, 5, 1, -3},
		{[]byte{0xA9, 0x3E, 0x02}, -7, 0, -7},
		{[]byte{0xA9, 0x3E, 0x03}, 0, 0, 0},
		{[]byte{0xA9, 0x3E, 0x21}, -5634879008887978, -327993, -2175620266},
		{[]byte{0xA9, 0x3E, 0x01}, -5634879008887978, -1408719752221994, -2},
		{[]byte{0xA9, 0x3E, 0x01}, -7, -1, -3},
		{[]byte{0xA9, 0x3E, 0x01}, -13, -3, -1},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> code: %x x: %d arg: q %d, r %d", test.code, test.x, test.q, test.r)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))

			codeCell := cell.BeginCell().MustStoreBinarySnake(test.code).EndCell()
			codeSlice := codeCell.BeginParse()

			op := RSHIFTCCODEMOD(0)
			op.Deserialize(codeSlice)

			err := op.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal("Failed RSHIFTCCODEMOD execution:", err.Error())
			}

			r, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop remainder:", err.Error())
			}

			if r.Cmp(big.NewInt(test.r)) != 0 {
				t.Errorf("Expected remainder %d, got %d", test.r, r)
			}

			q, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed to pop quotient:", err.Error())
			}

			if q.Cmp(big.NewInt(test.q)) != 0 {
				t.Errorf("Expected quotient %d, got %d", test.q, q)
			}
		})
	}
}
