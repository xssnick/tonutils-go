package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return HASHSU() })
}

func HASHSU() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			s, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			cl := s.ToBuilder().EndCell()
			hash := cl.HashKey()
			return state.Stack.PushOwnedInt(new(big.Int).SetBytes(hash[:]))
		},
		Name:      "HASHSU",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x01),
	}
}
