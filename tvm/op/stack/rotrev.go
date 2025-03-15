package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ROTREV() })
}

func ROTREV() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			c, err := state.Stack.Pop()
			if err != nil {
				return err
			}
			b, err := state.Stack.Pop()
			if err != nil {
				return err
			}
			a, err := state.Stack.Pop()
			if err != nil {
				return err
			}
			if err = state.Stack.Push(c); err != nil {
				return err
			}
			if err = state.Stack.Push(a); err != nil {
				return err
			}
			if err = state.Stack.Push(b); err != nil {
				return err
			}
			return nil
		},
		Name:   "ROTREV",
		Prefix: []byte{0x59},
	}
}
