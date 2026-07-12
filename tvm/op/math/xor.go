package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return XOR() })
}

func XOR() *helpers.SimpleOP {
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

			// int64 fast path: XOR of two int64 values stays within int64,
			// operands (possibly shared statics) are never mutated.
			if i0 != nil && i1 != nil && i0.IsInt64() && i1.IsInt64() {
				return state.Stack.PushSmallInt(i0.Int64() ^ i1.Int64())
			}

			return pushBinaryIntResult(state, i0, i1, func(x, y *big.Int) *big.Int {
				return new(big.Int).Xor(x, y)
			})
		},
		Name:      "XOR",
		BitPrefix: helpers.BytesPrefix(0xB2),
	}
}
