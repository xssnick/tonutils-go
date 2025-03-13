package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return IF() })
}

func IF() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			c0, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			b1, err := state.Stack.PopBool()
			if err != nil {
				return err
			}

			if b1 {
				return state.Call(c0)
			}
			return nil
		},
		Name:   "IF",
		Prefix: []byte{0xDE},
	}
}
