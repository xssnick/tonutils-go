package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, blkSwapOp)
}

// Both counts are encoded one less than they mean, so the operand is the raw
// instruction byte and the interpreter adds the bias back.
var blkSwapOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0x55)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		i, j := unpackArgs2(args)
		x, y := i+1, j+1
		if x+y > state.Stack.Len() {
			return vmerr.Error(vmerr.CodeStackUnderflow)
		}
		if err := state.Stack.Reverse(x+y, y); err != nil {
			return err
		}
		if err := state.Stack.Reverse(y, 0); err != nil {
			return err
		}
		return state.Stack.Reverse(x+y, 0)
	},
	Name: func(args uint64) string {
		i, j := unpackArgs2(args)
		return fmt.Sprintf("%d,%d BLKSWAP", i+1, j+1)
	},
})

func BLKSWAP(i, j uint8) vm.OP {
	return vm.Bind(blkSwapOp, packArgs2(i-1, j-1))
}
