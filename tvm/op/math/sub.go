package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SUB() })
}

func SUB() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			i1, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			return pushBinaryIntResult(state, i1, i0, func(x, y *big.Int) *big.Int {
				return x.Sub(x, y)
			})
		},
		Name:      "SUB",
		BitPrefix: helpers.BytesPrefix(0xA1),
	}
}
