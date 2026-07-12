package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return GEQ() })
}

func GEQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}

			return pushCompareResult(state, x, y, func(x, y *big.Int) bool {
				return x.Cmp(y) != -1
			})
		},
		Name:      "GEQ",
		BitPrefix: helpers.BytesPrefix(0xBE),
	}
}
