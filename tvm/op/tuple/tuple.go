package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, tupleOp)
}

var tupleOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x6f0, 12)),
	ArgBits:  4,
	Name: func(args uint64) string {
		return fmt.Sprintf("%d TUPLE", uint8(args))
	},
	Action: func(state *vm.State, args uint64) error {
		return execMakeTuple(state, int(uint8(args)))
	},
})

func TUPLE(n uint8) vm.OP {
	return vm.Bind(tupleOp, uint64(n))
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
		val, err := state.Stack.PopAny()
		if err != nil {
			return err
		}
		vals[count-1-i] = val
	}

	newTuple := tuplepkg.NewTupleOwned(vals)
	return state.PushTupleCharged(newTuple)
}
