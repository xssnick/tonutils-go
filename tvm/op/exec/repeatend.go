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
			countVal, err := state.Stack.PopIntRange(repeatCountMin, repeatCountMax)
			if err != nil {
				return err
			}

			count := countVal.Int64()
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
		Name:   "REPEATEND",
		Prefix: []byte{0xE5},
	}
}
