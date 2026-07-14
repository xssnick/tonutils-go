package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DIVMOD() })
}

func DIVMOD() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntRead()
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

			q, r := helpers.DivFloor(x, y)

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:      "DIVMOD",
		BitPrefix: helpers.BytesPrefix(0xA9, 0x0C),
	}
}
