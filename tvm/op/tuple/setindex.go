package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SETINDEX(0) })
}

func SETINDEX(n uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x6f5, 12).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("%d SETINDEX", n)
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
			return execSetIndex(state, int(n))
		},
	}
}

func execSetIndex(state *vm.State, idx int) error {
	val, err := state.Stack.PopAny()
	if err != nil {
		return err
	}
	tup, err := state.Stack.PopTupleRange(255)
	if err != nil {
		return err
	}
	if idx >= tup.Len() || idx < 0 {
		return vmerr.Error(vmerr.CodeRangeCheck, "tuple index out of range")
	}
	if err := (&tup).Set(idx, val); err != nil {
		return err
	}
	return state.Stack.PushTuple(tup)
}
