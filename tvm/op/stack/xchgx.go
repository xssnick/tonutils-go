package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return XCHGX() })
}

func XCHGX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := popSmallIndex(state)
			if err != nil {
				return err
			}
			return state.Stack.Exchange(0, idx)
		},
		Name:   "XCHGX",
		Prefix: []byte{0x67},
	}
}
