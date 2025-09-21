package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return ISTUPLE() })
}

func ISTUPLE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "ISTUPLE",
		Prefix: []byte{0x6f, 0x8a},
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			_, ok := val.(tuplepkg.Tuple)
			return state.Stack.PushBool(ok)
		},
	}
}
