package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return REPEATEND() })
}

func REPEATEND() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			count, err := state.Stack.PopIntRangeInt64(repeatCountMin, repeatCountMax)
			if err != nil {
				return err
			}

			if count <= 0 {
				return state.Return()
			}

			body, err := state.ExtractCurrentContinuation(0, -1, -1)
			if err != nil {
				return err
			}

			return state.Jump(&vm.RepeatContinuation{
				Count: count,
				Body:  body,
				After: state.Reg.C[0],
			})
		},
		Name:      "REPEATEND",
		BitPrefix: helpers.BytesPrefix(0xE5),
	}
}
