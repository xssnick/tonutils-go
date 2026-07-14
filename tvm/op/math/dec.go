package math

import (
	"math"
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
			i0, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}

			// int64 fast path: guarded against int64 overflow at MinInt64,
			// the operand (possibly a shared static) is never mutated.
			if i0 != nil && i0.IsInt64() {
				if v := i0.Int64(); v != math.MinInt64 {
					return state.Stack.PushSmallInt(v - 1)
				}
			}

			return pushUnaryIntResult(state, i0, func(x *big.Int) *big.Int {
				return new(big.Int).Sub(x, bigIntOne)
			})
		},
		Name:      "DEC",
		BitPrefix: helpers.BytesPrefix(0xA5),
	}
}
