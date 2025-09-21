package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return INDEXQ(0) })
}

func INDEXQ(n uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x6f6, 12).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("%d INDEXQ", n)
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
			return execIndexQuiet(state, int(n))
		},
	}
}

func execIndexQuiet(state *vm.State, idx int) error {
	tup, err := state.Stack.PopMaybeTupleRange(255)
	if err != nil {
		return err
	}
	if tup == nil || idx >= tup.Len() || idx < 0 {
		return state.Stack.PushAny(nil)
	}
	val, err := tup.Index(idx)
	if err != nil {
		return err
	}
	return state.Stack.PushAny(val)
}
