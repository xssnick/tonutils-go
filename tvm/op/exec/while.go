package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return WHILE() })
}

func WHILE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			body, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			cond, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			after, err := state.ExtractCurrentContinuation(1, -1, -1)
			if err != nil {
				return err
			}

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
		Name:   "WHILE",
		Prefix: []byte{0xE8},
	}
}
