package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return XCHG3(0, 0, 0) })
}

func XCHG3(i, j, k uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			if err := state.Stack.Exchange(2, int(i)); err != nil {
				return err
			}
			if err := state.Stack.Exchange(1, int(j)); err != nil {
				return err
			}

			return state.Stack.Exchange(0, int(k))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d,%d XCHG3", i, j, k)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0x4}, 4).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(i), 4).MustStoreUInt(uint64(j), 4).MustStoreUInt(uint64(k), 4)
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
			kval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(ival)
			j = uint8(jval)
			k = uint8(kval)
			return nil
		},
	}
	return op
}
