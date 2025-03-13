package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return BLKDROP(0) })
}

func BLKDROP(num uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			return state.Stack.Drop(int(num))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d BLKDROP", num)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0x5F, 0x00}, 12).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(num), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			num = uint8(val)
			return nil
		},
	}
	return op
}
