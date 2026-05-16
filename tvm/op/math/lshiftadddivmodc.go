package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LSHIFTADDDIVMODC() })
}

func LSHIFTADDDIVMODC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 4); err != nil {
				return err
			}
			y, err := popIntRange(state, 0, 256)
			if err != nil {
				return err
			}
			z, err := popInt(state)
			if err != nil {
				return err
			}
			w, err := popInt(state)
			if err != nil {
				return err
			}
			x, err := popInt(state)
			if err != nil {
				return err
			}
			if err = requireFiniteInts(z, w, x); err != nil {
				return err
			}

			if z.Sign() == 0 {
				// division by 0
				return vmerr.VMError{
					Code: vmerr.CodeIntOverflow,
					Msg:  "division by zero",
				}
			}

			dividend := x.Add(y.Mul(x, y.Lsh(big.NewInt(1), uint(y.Uint64()))), w)
			q := helpers.DivCeil(dividend, z)
			r := x.Sub(dividend, z.Mul(z, q))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:      "LSHIFTADDDIVMODC",
		BitPrefix: helpers.BytesPrefix(0xA9, 0xC2),
	}
}
