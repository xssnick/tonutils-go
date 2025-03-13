package funcs

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return NOW() })
}

func NOW() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			v, err := state.GetParam(3)
			if err != nil {
				return err
			}
			return state.Stack.Push(v)
		},
		Name:   "NOW",
		Prefix: []byte{0xF8, 0x23},
	}
}
