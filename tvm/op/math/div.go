package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DIV() })
}

func DIV() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			i1, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			if i1.Sign() == 0 {
				// division by 0
				return vmerr.VMError{
					Code: vmerr.ErrIntOverflow.Code,
					Msg:  "division by zero",
				}
			}

			return state.Stack.PushInt(i0.Div(i0, i1))
		},
		Name:   "DIV",
		Prefix: []byte{0xA9, 0x04},
	}
}
