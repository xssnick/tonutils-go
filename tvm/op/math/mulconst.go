package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULCONST() })
}

func MULCONST() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			_, err := state.CurrentCode.LoadUInt(8) // skip 0xA7
			if err != nil {
				return err
			}

			cc, err := state.CurrentCode.LoadInt(8)
			if err != nil {
				return err
			}

			constant := big.NewInt(int64(cc))

			i0, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			return state.Stack.PushInt(i0.Mul(i0, constant))
		},
		Name:   "MULCONST",
		Prefix: []byte{0xA7},
	}
}
