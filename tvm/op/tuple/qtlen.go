package tuple

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return QTLEN() })
}

func QTLEN() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   "QTLEN",
		Prefix: []byte{0x6f, 0x89},
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			if tup, ok := val.(tuplepkg.Tuple); ok {
				return state.Stack.PushInt(big.NewInt(int64(tup.Len())))
			}
			return state.Stack.PushInt(big.NewInt(-1))
		},
	}
}
