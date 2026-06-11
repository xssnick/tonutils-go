package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ROT() })
}

func ROT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := requireStackDepth(state, 3); err != nil {
				return err
			}

			if err := state.Stack.Exchange(2, 1); err != nil {
				return err
			}
			return state.Stack.Exchange(1, 0)
		},
		Name:      "ROT",
		BitPrefix: helpers.BytesPrefix(0x58),
	}
}
