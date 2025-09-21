package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return REPEAT() })
}

func REPEAT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			body, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			i1, err := state.Stack.PopIntRange(repeatCountMin, repeatCountMax)
			if err != nil {
				return err
			}
			count := i1.Int64()

			if count < 0 {
				return nil
			}

			after, err := state.ExtractCurrentContinuation(1, -1, -1)
			if err != nil {
				return err
			}

			if count == 0 {
				return state.Jump(after)
			}

			return state.Jump(&vm.RepeatContinuation{
				Count: count,
				Body:  body,
				After: after,
			})
		},
		Name:   "REPEAT",
		Prefix: []byte{0xE4},
	}
}
