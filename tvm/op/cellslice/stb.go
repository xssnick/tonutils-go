package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return STB() })
}

func STB() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}

			b0, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}

			b1, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}

			if err := b0.StoreBuilderUncheckedDepth(b1); err != nil {
				return err
			}
			return state.Stack.PushOwnedBuilder(b0)
		},
		Name:      "STB",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x13),
	}
}
