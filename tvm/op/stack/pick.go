package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return PICK() })
}

func PICK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := popSmallIndex(state)
			if err != nil {
				return err
			}
			if idx >= state.Stack.Len() {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			return state.Stack.PushAt(idx)
		},
		Name:   "PICK",
		Prefix: []byte{0x60},
	}
}
