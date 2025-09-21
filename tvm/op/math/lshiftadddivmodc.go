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
			y, err := state.Stack.PopIntRange(0, 256)
			if err != nil {
				return err
			}
			z, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			w, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
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
		Name:   "LSHIFTADDDIVMODC",
		Prefix: []byte{0xA9, 0xC2},
	}
}
