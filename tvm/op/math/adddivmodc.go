package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ADDDIVMODC() })
}

func ADDDIVMODC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 3); err != nil {
				return err
			}
			z, err := popInt(state)
			if err != nil {
				return err
			}
			w, err := popInt(state)
			if err != nil {
				return err
			}
			x, err := popInt(state)
			if err != nil {
				return err
			}
			if err = requireFiniteInts(z, w, x); err != nil {
				return err
			}

			if z.Sign() == 0 {
				// division by 0
				return vmerr.Error(vmerr.CodeIntOverflow, "division by zero")
			}

			sum := w.Add(x, w)
			q := helpers.DivCeil(sum, z)
			r := x.Sub(sum, z.Mul(z, q))

			err = state.Stack.PushInt(q)
			if err != nil {
				return err
			}

			return state.Stack.PushInt(r)
		},
		Name:       "ADDDIVMODC",
		BitPrefix:  helpers.BytesPrefix(0xA9, 0x02),
		MinVersion: 4,
	}
}
