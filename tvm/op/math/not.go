package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return NOT() },
		func() vm.OP { return QNOT() },
	)
}

func NOT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}

			// int64 fast path: NOT of an int64 value stays within int64,
			// the operand (possibly a shared static) is never mutated.
			if i0 != nil && i0.IsInt64() {
				return state.Stack.PushSmallInt(^i0.Int64())
			}

			return pushUnaryIntResult(state, i0, func(x *big.Int) *big.Int {
				return new(big.Int).Not(x)
			})
		},
		Name:      "NOT",
		BitPrefix: helpers.BytesPrefix(0xB3),
	}
}

func QNOT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			if i0 == nil {
				return pushNaNOrOverflow(state, true)
			}

			// int64 fast path: NOT of an int64 value stays within int64,
			// the operand (possibly a shared static) is never mutated.
			if i0.IsInt64() {
				return state.Stack.PushSmallInt(^i0.Int64())
			}

			// NOT never leaves the 257-bit range, so the quiet push cannot NaN.
			return state.Stack.PushOwnedIntQuiet(new(big.Int).Not(i0))
		},
		Name:      "QNOT",
		BitPrefix: helpers.BytesPrefix(0xB7, 0xB3),
	}
}
