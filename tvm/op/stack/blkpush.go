package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, blkPushOp)
}

var blkPushOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0x5F)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		i, j := unpackArgs2(args)
		for x := i; x != 0; x-- {
			val, err := state.Stack.Get(j)
			if err != nil {
				return err
			}
			if err := state.Stack.PushAny(val); err != nil {
				return err
			}
		}
		return nil
	},
	Name: func(args uint64) string {
		i, j := unpackArgs2(args)
		return fmt.Sprintf("%d, %d BLKPUSH", i, j)
	},
})

func BLKPUSH(i, j uint8) vm.OP {
	return vm.Bind(blkPushOp, packArgs2(i, j))
}
