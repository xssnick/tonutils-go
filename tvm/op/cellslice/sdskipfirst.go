package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SDSKIPFIRST() })
}

func SDSKIPFIRST() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntRange(0, 1023)
			if err != nil {
				return err
			}

			s1, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			if _, err = s1.LoadSlice(uint(i0.Uint64())); err != nil {
				return err
			}

			return state.Stack.PushSlice(s1)
		},
		Name:      "SDSKIPFIRST",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x21),
	}
}
