package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ENDC() })
}

func ENDC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			b, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			return state.Stack.PushCell(b.EndCell())
		},
		Name:   "ENDC",
		Prefix: []byte{0xC9},
	}
}
