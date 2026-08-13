package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, pushOp)
}

var pushOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x2, 4)),
	ArgBits:  4,
	Action: func(state *vm.State, args uint64) error {
		return state.Stack.PushAt(int(args))
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		prefix, err := code.LoadUInt(4)
		if err != nil {
			return 0, err
		}
		if prefix != 0x2 {
			return 0, vm.ErrCorruptedOpcode
		}
		return code.LoadUInt(4)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("s%d PUSH", args)
	},
})

func PUSH(stackIndex uint8) vm.OP {
	return vm.Bind(pushOp, uint64(stackIndex))
}
