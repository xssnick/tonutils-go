package stack

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DEPTH() })
}

func DEPTH() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return state.Stack.PushInt(big.NewInt(int64(state.Stack.Len())))
		},
		Name:   "DEPTH",
		Prefix: []byte{0x68},
	}
}
