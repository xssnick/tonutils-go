package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return REPEATBRK() })
	vm.List = append(vm.List, func() vm.OP { return UNTILBRK() })
	vm.List = append(vm.List, func() vm.OP { return WHILEBRK() })
}

func REPEATBRK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			body, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			countVal, err := state.Stack.PopIntRange(repeatCountMin, repeatCountMax)
			if err != nil {
				return err
			}

			count := countVal.Int64()
			if count <= 0 {
				return nil
			}

			after, err := state.ExtractCurrentContinuation(1, -1, -1)
			if err != nil {
				return err
			}
			afterCont := c1EnvelopeIf(state, true, after)

			return state.Jump(&vm.RepeatContinuation{
				Count: count,
				Body:  body,
				After: afterCont,
			})
		},
		Name:      "REPEATBRK",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x14),
	}
}

func UNTILBRK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			body, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			after, err := state.ExtractCurrentContinuation(1, -1, -1)
			if err != nil {
				return err
			}
			afterCont := c1EnvelopeIf(state, true, after)

			if cd := body.GetControlData(); cd == nil || cd.Save.C[0] == nil {
				state.Reg.C[0] = &vm.UntilContinuation{
					Body:  body,
					After: afterCont,
				}
			}

			return state.Jump(body)
		},
		Name:      "UNTILBRK",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x16),
	}
}

func WHILEBRK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			body, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			cond, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			after, err := state.ExtractCurrentContinuation(1, -1, -1)
			if err != nil {
				return err
			}
			afterCont := c1EnvelopeIf(state, true, after)

			if cd := cond.GetControlData(); cd == nil || cd.Save.C[0] == nil {
				state.Reg.C[0] = &vm.WhileContinuation{
					CheckCond: true,
					Body:      body,
					Cond:      cond,
					After:     afterCont,
				}
			}

			return state.Jump(cond)
		},
		Name:      "WHILEBRK",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x18),
	}
}
