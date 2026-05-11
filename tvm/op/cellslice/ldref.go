package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LDREF() })
}

func LDREF() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			s0, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			ref, err := s0.LoadRefCell()
			if err != nil {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}

			err = state.Stack.PushCell(ref)
			if err != nil {
				return err
			}
			return state.Stack.PushSlice(s0)
		},
		Name:      "LDREF",
		BitPrefix: helpers.BytesPrefix(0xD4),
	}
}
