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
	vm.List = append(vm.List, func() vm.OP { return TUPLE(0) })
}

func TUPLE(n uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x6f0, 12).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("%d TUPLE", n)
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
			return execMakeTuple(state, int(n))
		},
	}
}

func execMakeTuple(state *vm.State, count int) error {
	if count < 0 {
		return vmerr.Error(vmerr.CodeRangeCheck)
	}
	if state.Stack.Len() < count {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	vals := make([]any, count)
	for i := 0; i < count; i++ {
		val, err := state.Stack.Get(count - 1 - i)
		if err != nil {
			return err
		}
		vals[i] = val
	}

	if err := state.Stack.Drop(count); err != nil {
		return err
	}

	newTuple := tuplepkg.NewTuple(vals...)
	return state.Stack.PushTuple(*newTuple)
}
