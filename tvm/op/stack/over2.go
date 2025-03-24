package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return OVER2() })
}

func OVER2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := state.Stack.PushAny(state.Stack.Get(3)); err != nil {
				return err
			}
			if err := state.Stack.PushAny(state.Stack.Get(3)); err != nil {
				return err
			}
			return nil
		},
		Name:   "OVER2",
		Prefix: []byte{0x5D},
	}
}
