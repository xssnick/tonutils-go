package cellslice

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, plduOp)
}

// The operand is the encoded width minus one, exactly as it sits in the code.
var plduOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xD7, 0x0B)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		s0, err := state.Stack.PopSlice()
		if err != nil {
			return err
		}

		i, err := s0.PreloadBigUInt(uint(args) + 1)
		if err != nil {
			return err
		}

		return state.Stack.PushOwnedInt(i)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d PLDU", uint(args)+1)
	},
})

func PLDU(sz uint) vm.OP {
	return vm.Bind(plduOp, uint64(sz-1))
}
