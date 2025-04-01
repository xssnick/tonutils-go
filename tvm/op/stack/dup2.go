package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DUP2() })
}

func DUP2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			b, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			a, err := state.Stack.PopAny()
			if err != nil {
				return err
			}

			if err = state.Stack.PushAny(a); err != nil {
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
		Name:   "DUP2",
		Prefix: []byte{0x5C},
	}
}
