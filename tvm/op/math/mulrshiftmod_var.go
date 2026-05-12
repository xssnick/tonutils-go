package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULRSHIFTMOD_VAR() })
}

func MULRSHIFTMOD_VAR() *helpers.SimpleOP {
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

			q, _ := helpers.DivFloor(x.Mul(x, y), z.Lsh(big.NewInt(1), uint(z.Uint64())))
			r := new(big.Int).Sub(x, z.Mul(q, z))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:      "MULRSHIFTMOD_VAR",
		BitPrefix: helpers.BytesPrefix(0xA9, 0xAC),
	}
}
