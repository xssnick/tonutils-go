package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SETINDEXQ(0) })
}

func SETINDEXQ(n uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x6f7, 12).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("%d SETINDEXQ", n)
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
			return execSetIndexQuiet(state, int(n))
		},
	}
}

func execSetIndexQuiet(state *vm.State, idx int) error {
	val, err := state.Stack.PopAny()
	if err != nil {
		return err
	}
	tup, err := state.Stack.PopMaybeTupleRange(255)
	if err != nil {
		return err
	}
	if idx < 0 || idx >= 255 {
		return vmerr.Error(vmerr.CodeRangeCheck, "tuple index out of range")
	}

	length := 0
	if tup != nil {
		length = tup.Len()
	}
	if tup == nil {
		if val == nil {
			return state.Stack.PushAny(nil)
		}
		newTup := tuplepkg.NewTupleSized(idx + 1)
		if err := (&newTup).Set(idx, val); err != nil {
			return err
		}
		return state.Stack.PushTuple(newTup)
	}

	if idx >= length {
		if val == nil {
			return state.Stack.PushTuple(*tup)
		}
		tup.Resize(idx + 1)
	}

	if err := tup.Set(idx, val); err != nil {
		return err
	}

	return state.Stack.PushTuple(*tup)
}
