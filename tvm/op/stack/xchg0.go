package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return XCHG0(0) })
}

func XCHG0(i uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			return state.Stack.Exchange(int(0), int(i))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d XCHG0", i)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0x0}, 4).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(i), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(val)

			return nil
		},
	}
	return op
}
