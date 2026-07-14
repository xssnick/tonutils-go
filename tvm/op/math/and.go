package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return AND() })
}

func AND() *helpers.SimpleOP {
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

			// int64 fast path: AND of two int64 values stays within int64,
			// operands (possibly shared statics) are never mutated.
			if i0 != nil && i1 != nil && i0.IsInt64() && i1.IsInt64() {
				return state.Stack.PushSmallInt(i0.Int64() & i1.Int64())
			}

			return pushMaybeInt(state, versionedAndResult(state.GlobalVersion, i0, i1), false)
		},
		Name:      "AND",
		BitPrefix: helpers.BytesPrefix(0xB0),
	}
}
