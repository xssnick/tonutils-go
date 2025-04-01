package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RSHIFTC() })
}

func RSHIFTC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopIntRange(0, 1023)
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			res := helpers.DivCeil(x, y.Lsh(big.NewInt(1), uint(y.Uint64())))

			return state.Stack.PushInt(res)
		},
		Name:   "RSHIFTC",
		Prefix: []byte{0xA9, 0x26},
	}
}
