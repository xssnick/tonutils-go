package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return CONDSEL() })
}

func CONDSEL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x0, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			x1, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			i2, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			if i2.Sign() == 0 {
				return state.Stack.PushAny(x1)
			}
			return state.Stack.PushAny(x0)
		},
		Name:   "CONDSEL",
		Prefix: []byte{0xE3, 0x04},
	}
}
