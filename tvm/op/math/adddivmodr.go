package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ADDDIVMODR() })
}

func ADDDIVMODR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			z, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			w, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			if z.Sign() == 0 {
				return vmerr.ErrIntOverflow
			}

			sum := new(big.Int).Add(x, w)

			q := new(big.Int).Div(sum, z)
			r := new(big.Int).Sub(sum, new(big.Int).Mul(z, q))

			twoR := new(big.Int).Mul(r, big.NewInt(2))
			if twoR.Cmp(z) >= 0 {
				q.Add(q, big.NewInt(1))
				r.Sub(r, z)
			}

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:   "ADDDIVMODR",
		Prefix: []byte{0xA9, 0x01},
	}
}
