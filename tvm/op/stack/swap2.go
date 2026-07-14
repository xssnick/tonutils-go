package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SWAP2() })
}

func SWAP2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := requireStackDepth(state, 4); err != nil {
				return err
			}

			if err := state.Stack.Exchange(3, 1); err != nil {
				return err
			}
			return state.Stack.Exchange(2, 0)
		},
		Name:      "2SWAP",
		BitPrefix: helpers.BytesPrefix(0x5A),
	}
}
