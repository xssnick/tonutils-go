package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return OR() })
}

func OR() *helpers.SimpleOP {
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

			// int64 fast path: OR of two int64 values stays within int64,
			// operands (possibly shared statics) are never mutated.
			if i0 != nil && i1 != nil && i0.IsInt64() && i1.IsInt64() {
				return state.Stack.PushSmallInt(i0.Int64() | i1.Int64())
			}

			return pushMaybeInt(state, versionedOrResult(state.GlobalVersion, i0, i1), false)
		},
		Name:      "OR",
		BitPrefix: helpers.BytesPrefix(0xB1),
	}
}
