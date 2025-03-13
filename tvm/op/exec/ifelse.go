package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return IFELSE() })
}

func IFELSE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			c0, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			c1, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			b2, err := state.Stack.PopBool()
			if err != nil {
				return err
			}

			if b2 {
				return state.Call(c1)
			}
			return state.Call(c0)
		},
		Name:   "IFELSE",
		Prefix: []byte{0xE2},
	}
}
