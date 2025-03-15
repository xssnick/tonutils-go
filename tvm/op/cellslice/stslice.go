package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return STSLICE() })
}

func STSLICE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			b0, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}

			s1, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			if err := b0.StoreBuilder(s1.ToBuilder()); err != nil {
				return err
			}
			return state.Stack.PushBuilder(b0)
		},
		Name:   "STSLICE",
		Prefix: []byte{0xCE},
	}
}
