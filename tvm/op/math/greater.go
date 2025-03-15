package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return GREATER() })
}

func GREATER() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			i1, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			return state.Stack.PushBool(i0.Cmp(i1) == 1)
		},
		Name:   "GREATER",
		Prefix: []byte{0xBC},
	}
}
