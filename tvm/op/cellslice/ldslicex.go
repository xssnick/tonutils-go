package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LDSLICEX() })
}

func LDSLICEX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntRange(0, 1023)
			if err != nil {
				return err
			}

			s1, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			s, err := s1.LoadSlice(uint(i0.ToBigInt().Uint64()))
			if err != nil {
				return err
			}

			err = state.Stack.Push(s)
			if err != nil {
				return err
			}
			return state.Stack.Push(s1)
		},
		Name:   "LDSLICEX",
		Prefix: []byte{0xD7, 0x18},
	}
}
