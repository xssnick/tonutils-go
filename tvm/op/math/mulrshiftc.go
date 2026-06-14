package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULRSHIFTC() })
}

func MULRSHIFTC() *helpers.SimpleOP {
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
			if err = requireFiniteInts(y, x); err != nil {
				return err
			}

			q := helpers.DivCeil(x.Mul(x, y), z.Lsh(bigIntOne, uint(z.Uint64())))

			return state.Stack.PushInt(q)
		},
		Name:      "MULRSHIFTC",
		BitPrefix: helpers.BytesPrefix(0xA9, 0xA6),
	}
}
