package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ENDS() })
}

func ENDS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			s, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if s.RefsNum() > 0 || s.BitsLeft() > 0 {
				return vmerr.Error(vmerr.CodeCellUnderflow, "extra data remaining in deserialized cell")
			}
			return nil
		},
		Name:   "ENDS",
		Prefix: []byte{0xD1},
	}
}
