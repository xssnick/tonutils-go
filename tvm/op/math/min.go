package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MIN() })
}

func MIN() *helpers.SimpleOP {
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

			if i0.Cmp(i1) == -1 {
				return state.Stack.PushInt(i0)
			}
			return state.Stack.PushInt(i1)
		},
		Name:   "MIN",
		Prefix: []byte{0xB6, 0x08},
	}
}
