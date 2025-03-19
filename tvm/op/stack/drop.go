package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DROP() })
}

func DROP() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			_, err := state.Stack.PopAny()
			return err
		},
		Name:   "DROP",
		Prefix: []byte{0x30},
	}
}
