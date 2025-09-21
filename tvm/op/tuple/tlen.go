package tuple

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return TLEN() })
}

func TLEN() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "TLEN",
		Prefix: []byte{0x6f, 0x88},
		Action: func(state *vm.State) error {
			tup, err := state.Stack.PopTupleRange(255)
			if err != nil {
				return err
			}
			return state.Stack.PushInt(big.NewInt(int64(tup.Len())))
		},
	}
}
