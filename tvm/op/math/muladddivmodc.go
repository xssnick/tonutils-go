package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return MULADDDIVMODC() })
}

func MULADDDIVMODC() *helpers.SimpleOP {
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
			y, err := state.Stack.PopIntFinite()
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

			sum := x.Add(x.Mul(x, y), w)
			q := helpers.DivCeil(sum, z)
			r := y.Sub(sum, w.Mul(z, q))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:   "MULADDDIVMODC",
		Prefix: []byte{0xA9, 0x82},
	}
}
