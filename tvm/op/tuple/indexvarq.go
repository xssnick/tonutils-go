package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return INDEXVARQ() })
}

func INDEXVARQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:      "INDEXVARQ",
		BitPrefix: helpers.BytesPrefix(0x6f, 0x86),
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			idx, err := state.Stack.PopIntRange(0, 254)
			if err != nil {
				return err
			}
			return execIndexQuiet(state, int(idx.Int64()))
		},
	}
}
