package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return TPOP() })
}

func TPOP() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "TPOP",
		Prefix: []byte{0x6f, 0x8d},
		Action: func(state *vm.State) error {
			tup, err := state.Stack.PopTupleRange(255, 1)
			if err != nil {
				return err
			}
			val, err := tup.PopLast()
			if err != nil {
				return err
			}
			if err = state.Stack.PushTuple(tup); err != nil {
				return err
			}
			return state.Stack.PushAny(val)
		},
	}
}
