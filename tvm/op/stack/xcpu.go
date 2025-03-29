package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return XCPU(0, 0) })
}

func XCPU(i, j uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			if err := state.Stack.Exchange(0, int(i)); err != nil {
				return err
			}

			return state.Stack.PushAny(state.Stack.Get(j))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d XCPU", i, j)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0x51}, 8).EndCell(),
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
