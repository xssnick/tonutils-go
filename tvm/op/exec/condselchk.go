package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return CONDSELCHK() })
}

func CONDSELCHK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			if !sameStackValueType(x, y) {
				return vmerr.Error(vmerr.CodeTypeCheck, "two arguments of CONDSELCHK have different type")
			}
			cond, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			if cond {
				return state.Stack.PushAny(x)
			}
			return state.Stack.PushAny(y)
		},
		Name:      "CONDSELCHK",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x05),
	}
}
