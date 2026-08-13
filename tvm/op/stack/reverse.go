package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, reverseOp)
}

// The reversed block is at least two deep, so its length is encoded two less
// than it means; the operand stays the raw instruction byte.
var reverseOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0x5E)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		x, y := unpackArgs2(args)
		return state.Stack.Reverse(x+2+y, y)
	},
	Name: func(args uint64) string {
		x, y := unpackArgs2(args)
		return fmt.Sprintf("%d, %d REVERSE", x+2, y)
	},
})

func REVERSE(x, y uint8) vm.OP {
	return vm.Bind(reverseOp, packArgs2(x-2, y))
}
