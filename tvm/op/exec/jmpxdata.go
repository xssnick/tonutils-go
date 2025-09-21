package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return JMPXDATA() })
}

func JMPXDATA() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			if state.CurrentCode == nil {
				return vmerr.Error(vmerr.CodeTypeCheck, "current code is nil")
			}

			if err := state.Stack.PushSlice(state.CurrentCode.Copy()); err != nil {
				return err
			}

			return state.Jump(cont)
		},
		Name:   "JMPXDATA",
		Prefix: []byte{0xDB, 0x35},
	}
}
