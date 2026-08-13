package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, puxcOp)
}

var puxcOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0x52)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		i, j := unpackArgs2(args)
		if err := requireStackDepth(state, j, i); err != nil {
			return err
		}

		val, err := state.Stack.Get(i)
		if err != nil {
			return err
		}
		if err := state.Stack.PushAny(val); err != nil {
			return err
		}
		if err := state.Stack.Exchange(0, 1); err != nil {
			return err
		}

		return state.Stack.Exchange(0, j)
	},
	Name: func(args uint64) string {
		i, j := unpackArgs2(args)
		return fmt.Sprintf("%d,%d PUXC", i, j)
	},
})

func PUXC(i, j uint8) vm.OP {
	return vm.Bind(puxcOp, packArgs2(i, j))
}
