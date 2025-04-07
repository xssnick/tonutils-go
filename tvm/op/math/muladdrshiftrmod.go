package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULADDRSHIFTRMOD() })
}

func MULADDRSHIFTRMOD() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			z, err := state.Stack.PopIntRange(0, 256)
			if err != nil {
				return err
			}
			w, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			y, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			dividend := new(big.Int).Add(x.Mul(x, y), w)
			q := helpers.DivRound(dividend, z.Lsh(big.NewInt(1), uint(z.Uint64())))
			r := y.Sub(dividend, w.Mul(z, q))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:   "MULADDRSHIFTRMOD",
		Prefix: []byte{0xA9, 0xA1},
	}
}
