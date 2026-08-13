package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, jmpXArgsOp)
}

var jmpXArgsOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.SlicePrefix(12, []byte{0xDB, 0x10})),
	ArgBits:  4,
	Action: func(state *vm.State, args uint64) error {
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			return err
		}

		return state.JumpArgs(cont, int(int32(args)))
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("JMPXARGS %d", int32(args))
	},
})

func JMPXARGS(params int) vm.OP {
	return vm.Bind(jmpXArgsOp, uint64(uint32(params)))
}
