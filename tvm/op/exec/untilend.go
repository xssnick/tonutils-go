package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return UNTILEND() })
}

func UNTILEND() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			body, err := state.ExtractCurrentContinuation(0, -1, -1)
			if err != nil {
				return err
			}

			after := state.Reg.C[0]
			if cd := body.GetControlData(); cd == nil || cd.Save.C[0] == nil {
				state.Reg.C[0] = &vm.UntilContinuation{
					Body:  body,
					After: after,
				}
			}

			return state.Jump(body)
		},
		Name:   "UNTILEND",
		Prefix: []byte{0xE7},
	}
}
