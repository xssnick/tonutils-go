package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULRSHIFTCMOD_VAR() })
}

func MULRSHIFTCMOD_VAR() *helpers.SimpleOP {
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
			r := new(big.Int).Sub(x, z.Mul(q, z))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:      "MULRSHIFTCMOD_VAR",
		BitPrefix: helpers.BytesPrefix(0xA9, 0xAE),
	}
}
