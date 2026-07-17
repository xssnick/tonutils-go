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

			if err := state.Stack.Exchange(0, 2); err != nil {
				return err
			}
			f2, err := state.Stack.PopBool()
			if err != nil {
				// Match the original pop order: all three operands are consumed
				// when the condition has the wrong type.
				if dropErr := state.Stack.Drop(2); dropErr != nil {
					return dropErr
				}
				return err
			}

			if f2 {
				return state.Stack.DropMany(1, 1)
			}
			return state.Stack.Drop(1)
		},
		Name:      "CONDSEL",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x04),
	}
}
