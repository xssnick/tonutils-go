package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SETINDEXVARQ() })
}

func SETINDEXVARQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "SETINDEXVARQ",
		Prefix: []byte{0x6f, 0x87},
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopIntRange(0, 254)
			if err != nil {
				return err
			}
			return execSetIndexQuiet(state, int(idx.Int64()))
		},
	}
}
