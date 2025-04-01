package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return POW2() })
}

func POW2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopIntRange(0, 1023)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(y.Lsh(big.NewInt(1), uint(y.Uint64())))
		},
		Name:   "POW2",
		Prefix: []byte{0xAE},
	}
}
