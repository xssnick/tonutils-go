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
			b0, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}

			b1, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}

			if err := b0.StoreBuilder(b1); err != nil {
				return err
			}
			return state.Stack.Push(b0)
		},
		Name:   "STB",
		Prefix: []byte{0xCF, 0x13},
	}
}
