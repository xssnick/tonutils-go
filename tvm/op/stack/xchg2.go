package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, xchg2Op)
}

var xchg2Op = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0x50)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		i, j := unpackArgs2(args)
		if err := requireStackDepth(state, 2, i, j); err != nil {
			return err
		}

		if err := state.Stack.Exchange(1, i); err != nil {
			return err
		}

		return state.Stack.Exchange(0, j)
	},
	Name: func(args uint64) string {
		i, j := unpackArgs2(args)
		return fmt.Sprintf("%d,%d XCHG2", i, j)
	},
})

func XCHG2(i, j uint8) vm.OP {
	return vm.Bind(xchg2Op, packArgs2(i, j))
}
