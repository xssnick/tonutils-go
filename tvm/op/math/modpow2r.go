package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MODPOW2R() })
}

func MODPOW2R() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := popIntRange(state, 0, 256)
			if err != nil {
				return err
			}
			x, err := popIntFinite(state)
			if err != nil {
				return err
			}

			divider := y.Lsh(big.NewInt(1), uint(y.Uint64()))
			q := helpers.DivRound(x, divider)
			r := x.Sub(x, q.Mul(q, divider))

			return state.Stack.PushInt(r)
		},
		Name:      "MODPOW2R",
		BitPrefix: helpers.BytesPrefix(0xA9, 0x29),
	}
}
