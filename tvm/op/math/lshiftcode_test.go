package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestLshiftcodeOperation(t *testing.T) {
	tests := []struct {
		code []byte
		x    int64
		want int64
	}{
		{code: []byte{0xAA, 0x03}, x: 10, want: 160},
		{[]byte{0xAA, 0x04}, 10, 320},
		{[]byte{0xAA, 0x03}, -7, -112},
		{[]byte{0xAA, 0x02}, -5634879008887978, -45079032071103824},
		{[]byte{0xAA, 0x02}, -7, -56},
		{[]byte{0xAA, 0x02}, -13, -104},
	}

	st := vm.NewStack()

	for _, test := range tests {
		name := fmt.Sprintf("case -> code: %x x: %d arg: %d", test.code, test.x, test.want)
		t.Run(name, func(t *testing.T) {
			st.PushInt(big.NewInt(test.x))

			codeCell := cell.BeginCell().MustStoreBinarySnake(test.code).EndCell()
			codeSlice := codeCell.BeginParse()

			op := LSHIFTCODE(0)
			op.Deserialize(codeSlice)

			err := op.Interpret(&vm.State{
				Stack: st,
			})
			if err != nil {
				t.Fatal(err)
			}

			res, err := st.PopIntFinite()
			if err != nil {
				t.Fatal("Failed LSHIFT# pop: ", err.Error())
			}

			if res.Cmp(big.NewInt(test.want)) != 0 {
				t.Errorf("got %d, want %d\n", res, test.want)
			}
		})
	}
}
