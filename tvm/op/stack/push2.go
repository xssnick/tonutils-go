package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSH2(0, 0) })
}

func PUSH2(i, j uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			val, err := state.Stack.Get(int(i))
			if err != nil {
				return err
			}
			if err := state.Stack.PushAny(val); err != nil {
				return err
			}
			val, err = state.Stack.Get(int(j) + 1)
			if err != nil {
				return err
			}
			return state.Stack.PushAny(val)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d PUSH2", i, j)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0x53}, 8).EndCell(),
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
