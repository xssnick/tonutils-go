package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULRSHIFTR() })
}

func MULRSHIFTR() *helpers.SimpleOP {
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
			if x == nil || y == nil {
				return pushMaybeInt(state, legacyRShiftNaNResult(state.GlobalVersion, z.Uint64(), cppRoundNearest), false)
			}

			q := helpers.DivRound(x.Mul(x, y), z.Lsh(bigIntOne, uint(z.Uint64())))

			return state.Stack.PushInt(q)
		},
		Name:      "MULRSHIFTR",
		BitPrefix: helpers.BytesPrefix(0xA9, 0xA5),
	}
}
