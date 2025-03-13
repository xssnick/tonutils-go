package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LDREF() })
}

func LDREF() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			s0, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			ref, err := s0.LoadRef()
			if err != nil {
				return err
			}

			c, err := ref.ToCell()
			if err != nil {
				return err
			}

			err = state.Stack.Push(c)
			if err != nil {
				return err
			}
			return state.Stack.Push(s0)
		},
		Name:   "LDREF",
		Prefix: []byte{0xD4},
	}
}
