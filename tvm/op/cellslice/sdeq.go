package cellslice

import (
	"bytes"
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

			res := bytes.Equal(s0.MustToCell().Hash(), s1.MustToCell().Hash())
			return state.Stack.Push(res)
		},
		Name:   "SDEQ",
		Prefix: []byte{0xC7, 0x05},
	}
}
