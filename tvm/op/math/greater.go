package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return GREATER() })
}

func GREATER() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			return pushCompareResult(state, x, y, func(x, y *big.Int) bool {
				return x.Cmp(y) == 1
			})
		},
		Name:      "GREATER",
		BitPrefix: helpers.BytesPrefix(0xBC),
	}
}
