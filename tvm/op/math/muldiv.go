package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULDIV() })
}

func MULDIV() *helpers.SimpleOP {
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
				return vmerr.Error(vmerr.CodeIntOverflow, "division by zero")
			}

			q, _ := helpers.DivFloor(new(big.Int).Mul(x, y), z)

			return state.Stack.PushInt(q)
		},
		Name:   "MULDIV",
		Prefix: []byte{0xA9, 0x84},
	}
}
