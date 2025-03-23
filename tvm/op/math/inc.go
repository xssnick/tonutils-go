package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return INC() })
}

func INC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			return state.Stack.PushInt(i0.Add(i0, big.NewInt(1)))
		},
		Name:   "INC",
		Prefix: []byte{0xA4},
	}
}
