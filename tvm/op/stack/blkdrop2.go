package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return BLKDROP2(0, 0) })
}

func BLKDROP2(i, j uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			if err := state.Stack.Reverse(int(i)+int(j), 0); err != nil {
				return err
			}
			if err := state.Stack.Drop(int(i)); err != nil {
				return err
			}

			return state.Stack.Reverse(int(j), 0)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d BLKDROP2", i, j)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0x6C}, 8).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(i), 4).MustStoreUInt(uint64(j), 4)
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
			i = uint8(ival)
			j = uint8(jval)
			return nil
		},
	}
	return op
}
