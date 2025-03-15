package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ISZERO() })
}

func ISZERO() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			return state.Stack.PushBool(i0.Sign() == 0)
		},
		Name:   "ABS",
		Prefix: []byte{0xC0, 0x00},
	}
}
