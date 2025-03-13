package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/int257"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return HASHSU() })
}

func HASHSU() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			s, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			res := int257.NewInt257FromBytes(s.MustToCell().Hash())
			return state.Stack.Push(res)
		},
		Name:   "HASHSU",
		Prefix: []byte{0xF9, 0x01},
	}
}
