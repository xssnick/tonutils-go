package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DUP2() })
}

func DUP2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			a, err := state.Stack.Get(1)
			if err != nil {
				return err
			}
			b, err := state.Stack.Get(0)
			if err != nil {
				return err
			}

			if err = state.Stack.PushAny(a); err != nil {
				return err
			}

			return state.Stack.PushAny(b)
		},
		Name:   "2DUP",
		Prefix: []byte{0x5C},
	}
}
