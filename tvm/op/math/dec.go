package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DEC() })
}

func DEC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			return pushUnaryIntResult(state, i0, func(x *big.Int) *big.Int {
				return x.Sub(x, big.NewInt(1))
			})
		},
		Name:      "DEC",
		BitPrefix: helpers.BytesPrefix(0xA5),
	}
}
