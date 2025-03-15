package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return HASHCU() })
}

func HASHCU() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			c, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			return state.Stack.PushInt(new(big.Int).SetBytes(c.Hash()))
		},
		Name:   "HASHCU",
		Prefix: []byte{0xF9, 0x00},
	}
}
