package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHNULL() })
}

func PUSHNULL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:      "PUSHNULL",
		BitPrefix: helpers.BytesPrefix(0x6d),
		Action: func(state *vm.State) error {
			return state.Stack.PushAny(nil)
		},
	}
}
