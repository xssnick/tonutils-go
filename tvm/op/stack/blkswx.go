package stack

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return BLKSWX() })
}

func BLKSWX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := popSmallIndex(state)
			if err != nil {
				return err
			}
			x, err := popSmallIndex(state)
			if err != nil {
				return err
			}
			if x < 0 || y < 0 || x+y > state.Stack.Len() {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			if x == 0 || y == 0 {
				return nil
			}
			if err := state.Stack.Reverse(x+y-1, y); err != nil {
				return err
			}
			if err := state.Stack.Reverse(y-1, 0); err != nil {
				return err
			}
			return state.Stack.Reverse(x+y-1, 0)
		},
		Name:   "BLKSWX",
		Prefix: []byte{0x63},
	}
}
