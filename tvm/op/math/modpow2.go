package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MODPOW2() })
}

func MODPOW2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := popIntRange(state, 0, 256)
			if err != nil {
				return err
			}
			x, err := popIntFinite(state)
			if err != nil {
				return err
			}

			divider := y.Lsh(big.NewInt(1), uint(y.Uint64()))
			q := new(big.Int).Div(x, divider)
			r := x.Sub(x, q.Mul(q, divider))

			return state.Stack.PushInt(r)
		},
		Name:      "MODPOW2",
		BitPrefix: helpers.BytesPrefix(0xA9, 0x28),
	}
}
