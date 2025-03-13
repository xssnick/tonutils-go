package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MAX() })
}

func MAX() *helpers.SimpleOP {
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

			if i0.Cmp(i1) == 1 {
				return state.Stack.Push(i0)
			}
			return state.Stack.Push(i1)
		},
		Name:   "MAX",
		Prefix: []byte{0xB6, 0x09},
	}
}
