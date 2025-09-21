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
			if x == 0 {
				return nil
			}
			return state.Stack.Reverse(x+y-1, y)
		},
		Name:   "REVX",
		Prefix: []byte{0x64},
	}
}
