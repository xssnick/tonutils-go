package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SDEQ() })
}

func SDEQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			s0, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			s1, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			res := s1.LexCompare(s0) == 0
			return state.Stack.PushBool(res)
		},
		Name:      "SDEQ",
		BitPrefix: helpers.BytesPrefix(0xC7, 0x05),
	}
}
