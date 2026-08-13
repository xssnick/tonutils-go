package cellslice

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, ldiOp)
}

// The operand is the encoded width minus one, exactly as it sits in the code.
var ldiOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xD2)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		s0, err := state.Stack.PopSlice()
		if err != nil {
			return err
		}

		i, err := s0.LoadBigInt(uint(args) + 1)
		if err != nil {
			return err
		}

		err = state.Stack.PushOwnedInt(i)
		if err != nil {
			return err
		}
		return state.Stack.PushOwnedSlice(s0)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d LDI", uint(args)+1)
	},
})

func LDI(sz uint) vm.OP {
	return vm.Bind(ldiOp, uint64(sz-1))
}
