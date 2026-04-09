package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return AGAIN() })
	vm.List = append(vm.List, func() vm.OP { return AGAINEND() })
}

func AGAIN() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			body, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			return state.Jump(&vm.AgainContinuation{Body: body})
		},
		Name:      "AGAIN",
		BitPrefix: helpers.BytesPrefix(0xEA),
	}
}

func AGAINEND() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			body, err := state.ExtractCurrentContinuation(0, -1, -1)
			if err != nil {
				return err
			}
			return state.Jump(&vm.AgainContinuation{Body: body})
		},
		Name:      "AGAINEND",
		BitPrefix: helpers.BytesPrefix(0xEB),
	}
}
