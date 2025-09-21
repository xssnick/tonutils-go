package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SETINDEXVAR() })
}

func SETINDEXVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "SETINDEXVAR",
		Prefix: []byte{0x6f, 0x85},
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopIntRange(0, 254)
			if err != nil {
				return err
			}
			return execSetIndex(state, int(idx.Int64()))
		},
	}
}
