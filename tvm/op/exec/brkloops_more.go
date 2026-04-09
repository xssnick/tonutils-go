package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return REPEATENDBRK() },
		func() vm.OP { return UNTILENDBRK() },
		func() vm.OP { return WHILEENDBRK() },
		func() vm.OP { return AGAINBRK() },
		func() vm.OP { return AGAINENDBRK() },
	)
}

func REPEATENDBRK() *helpers.SimpleOP {
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
				After: c1EnvelopeIf(state, true, state.Reg.C[0]),
			})
		},
		Name:      "REPEATENDBRK",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x15),
	}
}

func UNTILENDBRK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			body, err := state.ExtractCurrentContinuation(0, -1, -1)
			if err != nil {
				return err
			}

			after := c1EnvelopeIf(state, true, state.Reg.C[0])
			if cd := body.GetControlData(); cd == nil || cd.Save.C[0] == nil {
				state.Reg.C[0] = &vm.UntilContinuation{
					Body:  body,
					After: after,
				}
			}

			return state.Jump(body)
		},
		Name:      "UNTILENDBRK",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x17),
	}
}

func WHILEENDBRK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cond, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			body, err := state.ExtractCurrentContinuation(0, -1, -1)
			if err != nil {
				return err
			}

			after := c1EnvelopeIf(state, true, state.Reg.C[0])
			if cd := cond.GetControlData(); cd == nil || cd.Save.C[0] == nil {
				state.Reg.C[0] = &vm.WhileContinuation{
					CheckCond: true,
					Body:      body,
					Cond:      cond,
					After:     after,
				}
			}

			return state.Jump(cond)
		},
		Name:      "WHILEENDBRK",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x19),
	}
}

func AGAINBRK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cc, err := state.ExtractCurrentContinuation(3, -1, -1)
			if err != nil {
				return err
			}
			state.Reg.C[1] = cc

			body, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			return state.Jump(&vm.AgainContinuation{Body: body})
		},
		Name:      "AGAINBRK",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x1A),
	}
}

func AGAINENDBRK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			c1SaveSet(state, true)

			body, err := state.ExtractCurrentContinuation(0, -1, -1)
			if err != nil {
				return err
			}
			return state.Jump(&vm.AgainContinuation{Body: body})
		},
		Name:      "AGAINENDBRK",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x1B),
	}
}
