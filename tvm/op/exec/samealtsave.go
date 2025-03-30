package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SAMEALTSAVE() })
}

func SAMEALTSAVE() (op *helpers.SimpleOP) {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			c0 := vm.ForceControlData(state.Reg.C[0])
			c0.GetControlData().Save.Define(1, state.Reg.C[1])
			state.Reg.C[0] = c0
			state.Reg.C[1] = c0.Copy()
			return nil
		},
		Name:   "SAMEALTSAVE",
		Prefix: []byte{0xED, 0xFB},
	}
}
