package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, setIndexOp)
}

var setIndexOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x6f5, 12)),
	ArgBits:  4,
	Name: func(args uint64) string {
		return fmt.Sprintf("%d SETINDEX", uint8(args))
	},
	Action: func(state *vm.State, args uint64) error {
		return execSetIndex(state, int(uint8(args)))
	},
})

func SETINDEX(n uint8) vm.OP {
	return vm.Bind(setIndexOp, uint64(n))
}

func execSetIndex(state *vm.State, idx int) error {
	if state.Stack.Len() < 2 {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
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
	return state.PushTupleCharged(tup)
}
