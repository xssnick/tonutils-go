package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ISPOS() })
}

func ISPOS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			
			return state.Stack.PushBool(i0.Sign() == 1)
		},
		Name:   "ISPOS",
		Prefix: []byte{0xC2, 0x00},
	}
}
