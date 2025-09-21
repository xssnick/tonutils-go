package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DIV() })
}

func DIV() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			if y.Sign() == 0 {
				// division by 0
				return vmerr.Error(vmerr.CodeIntOverflow, "division by zero")
			}

			q, _ := helpers.DivFloor(x, y)

			return state.Stack.PushInt(q)
		},
		Name:   "DIV",
		Prefix: []byte{0xA9, 0x04},
	}
}
