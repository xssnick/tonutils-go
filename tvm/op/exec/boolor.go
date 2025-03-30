package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return BOOLOR() })
}

func BOOLOR() (op *helpers.SimpleOP) {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			cont = vm.ForceControlData(cont)
			cont.GetControlData().Save.Define(1, val)
			return state.Stack.PushContinuation(cont)
		},
		Name:   "BOOLOR",
		Prefix: []byte{0xED, 0xF1},
	}
}
