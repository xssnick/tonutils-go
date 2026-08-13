package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, push2Op)
}

var push2Op = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0x53)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		i, j := unpackArgs2(args)
		if err := requireStackDepth(state, 0, i, j); err != nil {
			return err
		}

		val, err := state.Stack.Get(i)
		if err != nil {
			return err
		}
		if err := state.Stack.PushAny(val); err != nil {
			return err
		}
		val, err = state.Stack.Get(j + 1)
		if err != nil {
			return err
		}
		return state.Stack.PushAny(val)
	},
	Name: func(args uint64) string {
		i, j := unpackArgs2(args)
		return fmt.Sprintf("%d,%d PUSH2", i, j)
	},
})

func PUSH2(i, j uint8) vm.OP {
	return vm.Bind(push2Op, packArgs2(i, j))
}
