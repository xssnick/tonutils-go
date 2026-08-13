package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, xchg3Op, xchg3ExtOp)
}

// The same permutation has a compact 4-bit prefix and a long 12-bit one; they
// differ only in how much of the instruction the prefix takes.
var (
	xchg3Op = helpers.NewArgOP(&helpers.ArgOP{
		Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x4, 4)),
		ArgBits:  12,
		Action:   xchg3Action,
		Name:     xchg3Name,
	})
	xchg3ExtOp = helpers.NewArgOP(&helpers.ArgOP{
		Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x540, 12)),
		ArgBits:  12,
		Action:   xchg3Action,
		Name:     xchg3Name,
	})
)

func xchg3Action(state *vm.State, args uint64) error {
	i, j, k := unpackArgs3(args)
	if err := requireStackDepth(state, 3, i, j, k); err != nil {
		return err
	}

	if err := state.Stack.Exchange(2, i); err != nil {
		return err
	}
	if err := state.Stack.Exchange(1, j); err != nil {
		return err
	}

	return state.Stack.Exchange(0, k)
}

func xchg3Name(args uint64) string {
	i, j, k := unpackArgs3(args)
	return fmt.Sprintf("%d,%d,%d XCHG3", i, j, k)
}

func XCHG3(i, j, k uint8) vm.OP {
	return vm.Bind(xchg3Op, packArgs3(i, j, k))
}
