package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LSHIFTMODC() })
}

func LSHIFTMODC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			z, err := state.Stack.PopIntRange(0, 256)
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

			if y.Sign() == 0 {
				// division by 0
				return vmerr.VMError{
					Code: vmerr.CodeIntOverflow,
					Msg:  "division by zero",
				}
			}

			q := helpers.DivCeil(x.Mul(x, z.Lsh(big.NewInt(1), uint(z.Uint64()))), y)
			r := y.Sub(x, y.Mul(q, y))

			return state.Stack.PushInt(r)
		},
		Name:   "LSHIFTMODC",
		Prefix: []byte{0xA9, 0xCA},
	}
}
