package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return INDEX(0) })
}

func INDEX(n uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x6f1, 12).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("%d INDEX", n)
		},
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(n), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			n = uint8(val)
			return nil
		},
		Action: func(state *vm.State) error {
			tup, err := state.Stack.PopTupleRange(255)
			if err != nil {
				return err
			}
			v, err := tup.Index(int(n))
			if err != nil {
				return err
			}
			return state.Stack.PushAny(v)
		},
	}
}
