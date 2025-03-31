package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return UNTIL() })
}

func UNTIL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			body, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			after, err := state.ExtractCurrentContinuation(1, -1, -1)
			if err != nil {
				return err
			}

			if cd := body.GetControlData(); cd == nil || cd.Save.C[0] == nil {
				state.Reg.C[0] = &vm.UntilContinuation{
					Body:  body,
					After: after,
				}
			}

			return state.Jump(body)
		},
		Name:   "UNTIL",
		Prefix: []byte{0xE6},
	}
}
