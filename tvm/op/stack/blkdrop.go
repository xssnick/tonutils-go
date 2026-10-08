package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, blkDropOp)
}

var blkDropOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.SlicePrefix(12, []byte{0x5F, 0x00})),
	ArgBits:  4,
	Action: func(state *vm.State, args uint64) error {
		return state.Stack.Drop(int(args))
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d BLKDROP", args)
	},
})

func BLKDROP(num uint8) vm.OP {
	return vm.Bind(blkDropOp, uint64(num))
}
