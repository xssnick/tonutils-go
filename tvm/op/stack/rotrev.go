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
			if err := requireStackDepth(state, 3); err != nil {
				return err
			}

			c, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			b, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			a, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			if err = state.Stack.PushAny(c); err != nil {
				return err
			}
			if err = state.Stack.PushAny(a); err != nil {
				return err
			}
			if err = state.Stack.PushAny(b); err != nil {
				return err
			}
			return nil
		},
		Name:      "ROTREV",
		BitPrefix: helpers.BytesPrefix(0x59),
	}
}
