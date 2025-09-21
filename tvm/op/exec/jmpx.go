package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return JMPX() })
}

func JMPX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			return state.Jump(cont)
		},
		Name:   "JMPX",
		Prefix: []byte{0xD9},
	}
}
