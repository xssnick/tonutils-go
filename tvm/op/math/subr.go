package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SUBR() })
}

func SUBR() *helpers.SimpleOP {
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

			return pushBinaryIntResult(state, i0, i1, func(x, y *big.Int) *big.Int {
				return x.Sub(x, y)
			})
		},
		Name:      "SUBR",
		BitPrefix: helpers.BytesPrefix(0xA2),
	}
}
