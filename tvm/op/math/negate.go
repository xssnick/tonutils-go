package math

import (
	"errors"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

var minValue = new(big.Int).Lsh(big.NewInt(1), 256) // 2^256

func init() {
	vm.List = append(vm.List, func() vm.OP { return NEGATE() })
}

func NEGATE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			if i0.Cmp(minValue) == 0 {
				return errors.New("integer overflow: cannot negate -2^256")
			}

			return state.Stack.PushInt(i0.Neg(i0))
		},
		Name:   "NEGATE",
		Prefix: []byte{0xA3},
	}
}
