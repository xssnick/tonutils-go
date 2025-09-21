package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULMODC() })
}

func MULMODC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			z, err := state.Stack.PopIntFinite()
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

			if z.Sign() == 0 {
				// division by 0
				return vmerr.VMError{
					Code: vmerr.CodeIntOverflow,
					Msg:  "division by zero",
				}
			}

			q := helpers.DivCeil(x.Mul(x, y), z)
			r := x.Sub(x, z.Mul(z, q))

			return state.Stack.PushInt(r)
		},
		Name:   "MULMODC",
		Prefix: []byte{0xA9, 0x89},
	}
}
