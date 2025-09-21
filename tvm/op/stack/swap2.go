package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SWAP2() })
}

func SWAP2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			d, err := state.Stack.PopAny()
			if err != nil {
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
			if err = state.Stack.PushAny(d); err != nil {
				return err
			}
			if err = state.Stack.PushAny(a); err != nil {
				return err
			}

			return state.Stack.PushAny(b)
		},
		Name:   "2SWAP",
		Prefix: []byte{0x5A},
	}
}
