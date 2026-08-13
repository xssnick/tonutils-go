package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, xchg0LongOp)
}

var xchg0LongOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x11, 8)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		return state.Stack.Exchange(0, int(args))
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d XCHG0", args)
	},
})

func XCHG0L(i uint8) vm.OP {
	return vm.Bind(xchg0LongOp, uint64(i))
}
