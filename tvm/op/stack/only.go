package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ONLYTOPX() })
	vm.List = append(vm.List, func() vm.OP { return ONLYX() })
}

func ONLYTOPX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			count, err := popSmallIndex(state)
			if err != nil {
				return err
			}
			if count > state.Stack.Len() {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			return state.Stack.DropAfter(count)
		},
		Name:   "ONLYTOPX",
		Prefix: []byte{0x6a},
	}
}

func ONLYX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			count, err := popSmallIndex(state)
			if err != nil {
				return err
			}
			if count > state.Stack.Len() {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			return state.Stack.Drop(state.Stack.Len() - count)
		},
		Name:   "ONLYX",
		Prefix: []byte{0x6b},
	}
}
