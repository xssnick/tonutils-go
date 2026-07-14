package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return EXPLODEVAR() })
}

func EXPLODEVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:      "EXPLODEVAR",
		BitPrefix: helpers.BytesPrefix(0x6f, 0x84),
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			max, err := state.Stack.PopIntRangeInt64(0, 255)
			if err != nil {
				return err
			}
			return execExplode(state, int(max))
		},
	}
}
