package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return STGRAMS() })
}

func STGRAMS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}

			i0, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}

			b1, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}

			if err := b1.StoreBigCoins(i0); err != nil {
				return err
			}
			return state.Stack.PushOwnedBuilder(b1)
		},
		Name:      "STGRAMS",
		BitPrefix: helpers.BytesPrefix(0xFA, 0x02),
	}
}
