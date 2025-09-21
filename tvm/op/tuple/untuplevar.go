package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return UNTUPLEVAR() },
		func() vm.OP { return UNPACKFIRSTVAR() },
	)
}

func UNTUPLEVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "UNTUPLEVAR",
		Prefix: []byte{0x6f, 0x82},
		Action: func(state *vm.State) error {
			count, err := state.Stack.PopIntRange(0, 255)
			if err != nil {
				return err
			}
			return execUntuple(state, int(count.Int64()), true)
		},
	}
}

func UNPACKFIRSTVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "UNPACKFIRSTVAR",
		Prefix: []byte{0x6f, 0x83},
		Action: func(state *vm.State) error {
			count, err := state.Stack.PopIntRange(0, 255)
			if err != nil {
				return err
			}
			return execUntuple(state, int(count.Int64()), false)
		},
	}
}
