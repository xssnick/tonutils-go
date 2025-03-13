package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return EXECUTE() })
}

func EXECUTE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			c0, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			return state.Call(c0)
		},
		Name:   "EXECUTE",
		Prefix: []byte{0xD8},
	}
}
