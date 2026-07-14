package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LDSLICEX() })
}

func LDSLICEX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}

			i0, err := state.Stack.PopIntRangeInt64(0, 1023)
			if err != nil {
				return err
			}

			s1, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			s, err := s1.FetchSubslice(uint(i0), 0)
			if err != nil {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			if err = state.Stack.PushOwnedSlice(s); err != nil {
				return err
			}
			return state.Stack.PushOwnedSlice(s1)
		},
		Name:      "LDSLICEX",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x18),
	}
}
