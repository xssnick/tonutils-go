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
			return state.Stack.PushInt(new(big.Int).SetBytes(s.MustToCell().Hash()))
		},
		Name:   "HASHSU",
		Prefix: []byte{0xF9, 0x01},
	}
}
