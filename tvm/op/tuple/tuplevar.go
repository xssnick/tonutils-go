package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return TUPLEVAR() })
}

func TUPLEVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "TUPLEVAR",
		Prefix: []byte{0x6f, 0x80},
		Action: func(state *vm.State) error {
			count, err := state.Stack.PopIntRange(0, 255)
			if err != nil {
				return err
			}
			return execMakeTuple(state, int(count.Int64()))
		},
	}
}
