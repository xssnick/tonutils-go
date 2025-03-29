package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return TUCK() })
}

func TUCK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {

			b, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			a, err := state.Stack.PopAny()
			if err != nil {
				return err
			}

			if err = state.Stack.PushAny(b); err != nil {
				return err
			}
			if err = state.Stack.PushAny(a); err != nil {
				return err
			}

			return state.Stack.PushAny(b)
		},
		Name:   "TUCK",
		Prefix: []byte{0x66},
	}
}
