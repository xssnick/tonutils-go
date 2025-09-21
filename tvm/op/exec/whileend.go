package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return WHILEEND() })
}

func WHILEEND() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cond, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			body, err := state.ExtractCurrentContinuation(0, -1, -1)
			if err != nil {
				return err
			}

			after := state.Reg.C[0]
			if cd := cond.GetControlData(); cd == nil || cd.Save.C[0] == nil {
				state.Reg.C[0] = &vm.WhileContinuation{
					CheckCond: true,
					Body:      body,
					Cond:      cond,
					After:     after,
				}
			}

			return state.Jump(cond)
		},
		Name:   "WHILEEND",
		Prefix: []byte{0xE9},
	}
}
