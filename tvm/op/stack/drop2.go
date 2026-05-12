package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DROP2() })
}

func DROP2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return state.Stack.Drop(2)
		},
		Name:      "2DROP",
		BitPrefix: helpers.BytesPrefix(0x5B),
	}
}
