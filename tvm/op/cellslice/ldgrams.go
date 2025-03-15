package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LDGRAMS() })
}

func LDGRAMS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			s, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			coins, err := s.LoadBigCoins()
			if err != nil {
				return err
			}

			err = state.Stack.PushInt(coins)
			if err != nil {
				return err
			}
			return state.Stack.PushSlice(s)
		},
		Name:   "LDGRAMS",
		Prefix: []byte{0xFA, 0x00},
	}
}
