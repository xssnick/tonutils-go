package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return REVERSE(2, 0) })
}

func REVERSE(x, y uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			return state.Stack.Reverse(int(x)+int(y), int(y))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d, %d REVERSE", x, y)
		},
		BitPrefix: helpers.BytesPrefix(0x5E),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(x-2), 4).MustStoreUInt(uint64(y), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			xval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			yval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			x = uint8(xval + 2)
			y = uint8(yval)

			return nil
		},
	}
	return op
}
