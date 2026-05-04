package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MODR() })
}

func MODR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			if x == nil || y == nil {
				return pushNaNOrOverflow(state, false)
			}
			if y.Sign() == 0 {
				// division by 0
				return vmerr.Error(vmerr.CodeIntOverflow, "division by zero")
			}

			q := helpers.DivRound(x, y)
			r := x.Sub(x, y.Mul(y, q))

			return state.Stack.PushInt(r)
		},
		Name:      "MODR",
		BitPrefix: helpers.BytesPrefix(0xA9, 0x09),
	}
}
