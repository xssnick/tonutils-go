package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DUMPSTK() })
}

func DUMPSTK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			// TODO: log
			println("DUMPSTK:\n", state.Stack.String())
			return nil
		},
		Name:   "DUMPSTK",
		Prefix: []byte{0xFE, 0x00},
	}
}
