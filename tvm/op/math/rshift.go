package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RSHIFT() })
}

func RSHIFT() *helpers.SimpleOP {
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
				return pushMaybeInt(state, legacyShiftNaNResult(state.GlobalVersion, y.Uint64(), true), false)
			}

			return state.Stack.PushInt(new(big.Int).Rsh(x, uint(y.Uint64())))
		},
		Name:      "RSHIFT",
		BitPrefix: helpers.BytesPrefix(0xAD),
	}
}
