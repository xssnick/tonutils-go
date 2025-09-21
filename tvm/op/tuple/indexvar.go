package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return INDEXVAR() })
}

func INDEXVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "INDEXVAR",
		Prefix: []byte{0x6f, 0x81},
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopIntRange(0, 254)
			if err != nil {
				return err
			}
			tup, err := state.Stack.PopTupleRange(255)
			if err != nil {
				return err
			}
			v, err := tup.Index(int(idx.Int64()))
			if err != nil {
				return err
			}
			return state.Stack.PushAny(v)
		},
	}
}
