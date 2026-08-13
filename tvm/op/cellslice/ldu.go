package cellslice

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, lduOp)
}

// The operand is the encoded width minus one, exactly as it sits in the code.
var lduOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xD3)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		sz := uint(args) + 1

		s0, err := state.Stack.PopSlice()
		if err != nil {
			return err
		}

		if sz <= 64 {
			v, err := s0.LoadUInt(sz)
			if err != nil {
				return err
			}
			if v <= 1<<63-1 {
				err = state.Stack.PushSmallInt(int64(v))
			} else {
				err = state.Stack.PushOwnedInt(new(big.Int).SetUint64(v))
			}
			if err != nil {
				return err
			}

			return state.Stack.PushOwnedSlice(s0)
		}

		i, err := s0.LoadBigUInt(sz)
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
		return fmt.Sprintf("%d LDU", uint(args)+1)
	},
})

func LDU(sz uint) vm.OP {
	return vm.Bind(lduOp, uint64(sz-1))
}
