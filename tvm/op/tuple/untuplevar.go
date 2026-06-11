package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return UNTUPLEVAR() },
		func() vm.OP { return UNPACKFIRSTVAR() },
	)
}

func UNTUPLEVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:      "UNTUPLEVAR",
		BitPrefix: helpers.BytesPrefix(0x6f, 0x82),
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			count, err := state.Stack.PopIntRangeInt64(0, 255)
			if err != nil {
				return err
			}
			return execUntuple(state, int(count), true)
		},
	}
}

func UNPACKFIRSTVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:      "UNPACKFIRSTVAR",
		BitPrefix: helpers.BytesPrefix(0x6f, 0x83),
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			count, err := state.Stack.PopIntRangeInt64(0, 255)
			if err != nil {
				return err
			}
			return execUntuple(state, int(count), false)
		},
	}
}
