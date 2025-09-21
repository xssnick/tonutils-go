package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ROLL() })
}

func ROLL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := popSmallIndex(state)
			if err != nil {
				return err
			}
			if idx >= state.Stack.Len() {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			for idx > 0 {
				if err := state.Stack.Exchange(idx-1, idx); err != nil {
					return err
				}
				idx--
			}
			return nil
		},
		Name:   "ROLL",
		Prefix: []byte{0x61},
	}
}
