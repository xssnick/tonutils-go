package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return OR() })
}

func OR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			i1, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			return state.Stack.Push(i0.Or(i1))
		},
		Name:   "OR",
		Prefix: []byte{0xB1},
	}
}
