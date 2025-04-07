package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULRSHIFT() })
}

func MULRSHIFT() *helpers.SimpleOP {
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

			q, _ := helpers.DivFloor(x.Mul(x, y), z.Lsh(big.NewInt(1), uint(z.Uint64())))

			return state.Stack.PushInt(q)
		},
		Name:   "MULRSHIFT",
		Prefix: []byte{0xA9, 0xA4},
	}
}
