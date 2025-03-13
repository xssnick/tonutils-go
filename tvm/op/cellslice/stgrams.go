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
			i0, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			b1, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}

			if err := b1.StoreBigCoins(i0.ToBigInt()); err != nil {
				return err
			}
			return state.Stack.Push(b1)
		},
		Name:   "STGRAMS",
		Prefix: []byte{0xFA, 0x02},
	}
}
