package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return CONDSEL() })
}

func CONDSEL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 3 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			y0, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			x1, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			f2, err := state.Stack.PopBool()
			if err != nil {
				return err
			}

			if !f2 {
				return state.Stack.PushAny(y0)
			}
			return state.Stack.PushAny(x1)
		},
		Name:      "CONDSEL",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x04),
	}
}
