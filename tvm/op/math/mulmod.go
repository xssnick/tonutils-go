package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULMOD() })
}

func MULMOD() *helpers.SimpleOP {
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
					Code: vmerr.ErrIntOverflow.Code,
					Msg:  "division by zero",
				}
			}

			r := new(big.Int).Mod(x.Mul(x, y), z)

			return state.Stack.PushInt(r)
		},
		Name:   "MULMOD",
		Prefix: []byte{0xA9, 0x88},
	}
}
