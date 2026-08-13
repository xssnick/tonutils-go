package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, setIndexQOp)
}

var setIndexQOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x6f7, 12)),
	ArgBits:  4,
	Name: func(args uint64) string {
		return fmt.Sprintf("%d SETINDEXQ", uint8(args))
	},
	Action: func(state *vm.State, args uint64) error {
		if state.Stack.Len() < 2 {
			return vmerr.Error(vmerr.CodeStackUnderflow)
		}

		return execSetIndexQuiet(state, int(uint8(args)))
	},
})

func SETINDEXQ(n uint8) vm.OP {
	return vm.Bind(setIndexQOp, uint64(n))
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
		vals := make([]any, idx+1)
		vals[idx] = val
		newTup := tuplepkg.NewTupleOwned(vals)
		return state.PushTupleCharged(newTup)
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

	return state.PushTupleCharged(*tup)
}
