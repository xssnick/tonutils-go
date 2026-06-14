package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RSHIFTR() })
}

func RSHIFTR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}
			y, err := popIntRange(state, 0, 256)
			if err != nil {
				return err
			}
			x, err := popIntFinite(state)
			if err != nil {
				return err
			}

			res := helpers.DivRound(x, y.Lsh(bigIntOne, uint(y.Uint64())))

			return state.Stack.PushInt(res)
		},
		Name:      "RSHIFTR",
		BitPrefix: helpers.BytesPrefix(0xA9, 0x25),
	}
}
