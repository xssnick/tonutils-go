package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RSHIFTMOD() })
}

func RSHIFTMOD() *helpers.SimpleOP {
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

			divider := y.Lsh(bigIntOne, uint(y.Uint64()))
			q := new(big.Int).Div(x, divider)
			r := x.Sub(x, y.Mul(q, divider))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:      "RSHIFTMOD",
		BitPrefix: helpers.BytesPrefix(0xA9, 0x2C),
	}
}
