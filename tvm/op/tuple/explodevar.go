package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return EXPLODEVAR() })
}

func EXPLODEVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "EXPLODEVAR",
		Prefix: []byte{0x6f, 0x84},
		Action: func(state *vm.State) error {
			max, err := state.Stack.PopIntRange(0, 255)
			if err != nil {
				return err
			}
			return execExplode(state, int(max.Int64()))
		},
	}
}
