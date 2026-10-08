package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, popLongOp)
}

var popLongOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x57, 8)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		return state.Stack.PopSwapAt(int(args))
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		prefix, err := code.LoadUInt(8)
		if err != nil {
			return 0, err
		}
		if prefix != 0x57 {
			return 0, vm.ErrCorruptedOpcode
		}
		return code.LoadUInt(8)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("s%d POP", args)
	},
})

func POPL(index uint8) vm.OP {
	return vm.Bind(popLongOp, uint64(index))
}
