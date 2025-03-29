package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return OVER2() })
}

func OVER2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 4 {
				return vmerr.ErrStackUnderflow
			}
			if err := state.Stack.PushAny(state.Stack.Get(3)); err != nil {
				return err
			}

			return state.Stack.PushAny(state.Stack.Get(3))
		},
		Name:   "OVER2",
		Prefix: []byte{0x5D},
	}
}
