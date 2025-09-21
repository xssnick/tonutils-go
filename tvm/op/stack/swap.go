package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SWAP() })
}

func SWAP() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return state.Stack.Exchange(0, 1)
		},
		Name:   "SWAP",
		Prefix: []byte{0x01},
	}
}
