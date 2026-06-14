package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULADDRSHIFTCMOD() })
}

func MULADDRSHIFTCMOD() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 4); err != nil {
				return err
			}
			z, err := popIntRange(state, 0, 256)
			if err != nil {
				return err
			}
			w, err := popInt(state)
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
			if err = requireFiniteInts(w, y, x); err != nil {
				return err
			}

			dividend := new(big.Int).Add(x.Mul(x, y), w)
			q := helpers.DivCeil(dividend, z.Lsh(bigIntOne, uint(z.Uint64())))
			r := y.Sub(dividend, w.Mul(z, q))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:      "MULADDRSHIFTCMOD",
		BitPrefix: helpers.BytesPrefix(0xA9, 0xA2),
	}
}
