package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DIVMODR() })
}

func DIVMODR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			if y.Sign() == 0 {
				// division by 0
				return vmerr.VMError{
					Code: vmerr.ErrIntOverflow.Code,
					Msg:  "division by zero",
				}
			}

			q := helpers.DivRound(x, y)
			r := new(big.Int).Sub(x, y.Mul(y, q))

			state.Stack.PushInt(q)

			return state.Stack.PushInt(r)
		},
		Name:   "DIVMODR",
		Prefix: []byte{0xA9, 0x0D},
	}
}
