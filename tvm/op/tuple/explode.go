package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, explodeOp)
}

var explodeOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x6f4, 12)),
	ArgBits:  4,
	Name: func(args uint64) string {
		return fmt.Sprintf("%d EXPLODE", uint8(args))
	},
	Action: func(state *vm.State, args uint64) error {
		return execExplode(state, int(uint8(args)))
	},
})

func EXPLODE(n uint8) vm.OP {
	return vm.Bind(explodeOp, uint64(n))
}

func execExplode(state *vm.State, max int) error {
	tup, err := state.Stack.PopTupleRange(max)
	if err != nil {
		return err
	}

	length := tup.Len()
	for i := 0; i < length; i++ {
		val, err := tup.Index(i)
		if err != nil {
			return err
		}
		if err = state.Stack.PushAny(val); err != nil {
			return err
		}
	}

	if err := state.ConsumeTupleGasLen(length); err != nil {
		return err
	}

	return state.Stack.PushSmallInt(int64(length))
}
