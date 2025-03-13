package funcs

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SETCP(0) })
}

func SETCP(cp int) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			state.CP = cp
			return nil
		},
		Name:   "SETCP",
		Prefix: []byte{0xFF, 0x00},
	}
}
