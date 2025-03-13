package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return IFNOTRET() })
}

func IFNOTRET() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			b0, err := state.Stack.PopBool()
			if err != nil {
				return err
			}

			if !b0 {
				return state.Return()
			}
			return nil
		},
		Name:   "IFNOTRET",
		Prefix: []byte{0xDD},
	}
}
