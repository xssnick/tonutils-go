package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return BLKSWAP(1, 1) })
}

func BLKSWAP(i, j uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			x, y := int(i), int(j)
			if x+y > state.Stack.Len() {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			if err := state.Stack.Reverse(x+y, y); err != nil {
				return err
			}
			if err := state.Stack.Reverse(y, 0); err != nil {
				return err
			}
			return state.Stack.Reverse(x+y, 0)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d BLKSWAP", i, j)
		},
		BitPrefix: helpers.BytesPrefix(0x55),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(i-1), 4).MustStoreUInt(uint64(j-1), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			ival, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			jval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(ival + 1)
			j = uint8(jval + 1)

			return nil
		},
	}
	return op
}
