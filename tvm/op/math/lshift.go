package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LSHIFT() })
}

func LSHIFT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := popIntRange(state, 0, 1023)
			if err != nil {
				return err
			}
			x, err := popIntRead(state)
			if err != nil {
				return err
			}
			if x == nil {
				return pushMaybeInt(state, legacyShiftNaNResult(state.GlobalVersion, y.Uint64(), false), false)
			}

			return pushMaybeInt(state, leftShiftResult(x, y.Uint64()), false)
		},
		Name:      "LSHIFT",
		BitPrefix: helpers.BytesPrefix(0xAC),
	}
}
