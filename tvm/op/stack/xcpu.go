package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, xcpuOp)
}

var xcpuOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0x51)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		i, j := unpackArgs2(args)
		if err := requireStackDepth(state, 0, i, j); err != nil {
			return err
		}

		if err := state.Stack.Exchange(0, i); err != nil {
			return err
		}
		val, err := state.Stack.Get(j)
		if err != nil {
			return err
		}
		return state.Stack.PushAny(val)
	},
	Name: func(args uint64) string {
		i, j := unpackArgs2(args)
		return fmt.Sprintf("%d,%d XCPU", i, j)
	},
})

func XCPU(i, j uint8) vm.OP {
	return vm.Bind(xcpuOp, packArgs2(i, j))
}
