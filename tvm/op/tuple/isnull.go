package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ISNULL() })
}

func ISNULL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "ISNULL",
		Prefix: []byte{0x6e},
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			return state.Stack.PushBool(val == nil)
		},
	}
}
