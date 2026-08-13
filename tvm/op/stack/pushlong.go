package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, pushLongOp)
}

var pushLongOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x56, 8)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		return state.Stack.PushAt(int(args))
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		prefix, err := code.LoadUInt(8)
		if err != nil {
			return 0, err
		}
		if prefix != 0x56 {
			return 0, vm.ErrCorruptedOpcode
		}
		return code.LoadUInt(8)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("s%d PUSH", args)
	},
})

func PUSHL(index uint8) vm.OP {
	return vm.Bind(pushLongOp, uint64(index))
}
