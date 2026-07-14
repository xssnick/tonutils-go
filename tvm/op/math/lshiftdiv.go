package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LSHIFTDIV() })
}

func LSHIFTDIV() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 3); err != nil {
				return err
			}
			z, err := popIntRange(state, 0, 256)
			if err != nil {
				return err
			}
			y, err := popInt(state)
			if err != nil {
				return err
			}
			x, err := popInt(state)
			if err != nil {
				return err
			}
			x = legacyLeftShiftOperand(state.GlobalVersion, x, z.Uint64())
			if err = requireFiniteInts(y, x); err != nil {
				return err
			}

			if y.Sign() == 0 {
				// division by 0
				return vmerr.VMError{
					Code: vmerr.CodeIntOverflow,
					Msg:  "division by zero",
				}
			}

			q, _ := helpers.DivFloor(x.Mul(x, z.Lsh(bigIntOne, uint(z.Uint64()))), y)

			return state.Stack.PushInt(q)
		},
		Name:      "LSHIFTDIV",
		BitPrefix: helpers.BytesPrefix(0xA9, 0xC4),
	}
}
