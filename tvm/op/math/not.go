package math

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return NOT() },
		func() vm.OP { return QNOT() },
	)
}

func NOT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			return state.Stack.PushInt(i0.Not(i0))
		},
		Name:      "NOT",
		BitPrefix: helpers.BytesPrefix(0xB3),
	}
}

func QNOT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			if i0 == nil {
				return pushNaNOrOverflow(state, true)
			}
			return state.Stack.PushIntQuiet(i0.Not(i0))
		},
		Name:      "QNOT",
		BitPrefix: helpers.BytesPrefix(0xB7, 0xB3),
	}
}
