package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LDMSGADDR() })
}

func LDMSGADDR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			s0, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			addr, err := s0.LoadAddr()
			if err != nil {
				return err
			}

			err = state.Stack.PushSlice(cell.BeginCell().MustStoreAddr(addr).ToSlice())
			if err != nil {
				return err
			}
			return state.Stack.PushSlice(s0)
		},
		Name:   "LDMSGADDR",
		Prefix: []byte{0xFA, 0x40},
	}
}
