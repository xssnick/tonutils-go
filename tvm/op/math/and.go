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
			i0, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			i1, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			return pushMaybeInt(state, versionedAndResult(state.GlobalVersion, i0, i1), false)
		},
		Name:      "AND",
		BitPrefix: helpers.BytesPrefix(0xB0),
	}
}
