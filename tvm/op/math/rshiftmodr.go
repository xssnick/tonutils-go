package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RSHIFTMODR() })
}

func RSHIFTMODR() *helpers.SimpleOP {
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
			q := helpers.DivRound(x, divider)
			r := x.Sub(x, y.Mul(q, divider))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:   "RSHIFTMODR",
		Prefix: []byte{0xA9, 0x2D},
	}
}
