package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return TPUSH() })
}

func TPUSH() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "TPUSH",
		Prefix: []byte{0x6f, 0x8c},
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			tup, err := state.Stack.PopTupleRange(254)
			if err != nil {
				return err
			}
			tup.Append(val)
			return state.Stack.PushTuple(tup)
		},
	}
}
