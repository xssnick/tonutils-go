package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return EQUAL() })
}

func EQUAL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			i0, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			i1, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}

			return pushCompareResult(state, i0, i1, func(x, y *big.Int) bool {
				return x.Cmp(y) == 0
			})
		},
		Name:      "EQUAL",
		BitPrefix: helpers.BytesPrefix(0xBA),
	}
}
