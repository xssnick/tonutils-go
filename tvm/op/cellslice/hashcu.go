package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/int257"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return HASHCU() })
}

func HASHCU() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			c, err := state.Stack.PopCell()
			if err != nil {
				return err
			}

			res := int257.NewInt257FromBytes(c.Hash())
			return state.Stack.Push(res)
		},
		Name:   "HASHCU",
		Prefix: []byte{0xF9, 0x00},
	}
}
