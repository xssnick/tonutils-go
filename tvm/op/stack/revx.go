package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return REVX() })
}

func REVX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			y, err := popSmallIndex(state)
			if err != nil {
				return err
			}
			x, err := popSmallIndex(state)
			if err != nil {
				return err
			}
			if x < 0 || y < 0 || x+y > state.Stack.Len() {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			if err := consumeLargeStackMoveGas(state, x); err != nil {
				return err
			}
			return state.Stack.Reverse(x+y, y)
		},
		Name:      "REVX",
		BitPrefix: helpers.BytesPrefix(0x64),
	}
}
