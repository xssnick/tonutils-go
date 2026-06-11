package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return INC() })
}

func INC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			return pushUnaryIntResult(state, i0, func(x *big.Int) *big.Int {
				return x.Add(x, bigIntOne)
			})
		},
		Name:      "INC",
		BitPrefix: helpers.BytesPrefix(0xA4),
	}
}
