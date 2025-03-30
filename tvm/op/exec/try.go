package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return TRY() })
}

func TRY() (op *helpers.SimpleOP) {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			handler, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			oldC2 := state.Reg.C[2]
			cc, err := state.ExtractCurrentContinuation(1, -1, 7)

			handler = vm.ForceControlData(handler)
			handler.GetControlData().Save.Define(2, oldC2)
			handler.GetControlData().Save.Define(0, cc)
			state.Reg.C[0] = cc
			state.Reg.C[2] = handler

			return state.Jump(cont)
		},
		Name:   "TRY",
		Prefix: []byte{0xF2, 0xFF},
	}
}
