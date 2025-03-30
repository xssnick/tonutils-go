package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RETALT() })
}

func RETALT() (op *helpers.SimpleOP) {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return state.ReturnAlt()
		},
		Name:   "RETALT",
		Prefix: []byte{0xDB, 0x31},
	}
}
