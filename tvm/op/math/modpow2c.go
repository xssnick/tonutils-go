package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MODPOW2C() })
}

func MODPOW2C() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopIntRange(0, 1023)
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			divider := y.Lsh(big.NewInt(1), uint(y.Uint64()))
			q := helpers.DivCeil(x, divider)
			r := x.Sub(x, q.Mul(q, divider))

			return state.Stack.PushInt(r)
		},
		Name:   "MODPOW2C",
		Prefix: []byte{0xA9, 0x2A},
	}
}
