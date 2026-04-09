package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return BOOLAND() },
		func() vm.OP { return COMPOSBOTH() },
		func() vm.OP { return ATEXIT() },
		func() vm.OP { return ATEXITALT() },
		func() vm.OP { return SETEXITALT() },
		func() vm.OP { return THENRET() },
		func() vm.OP { return THENRETALT() },
		func() vm.OP { return INVERT() },
		func() vm.OP { return BOOLEVAL() },
	)
}

func composeCont(mask uint8, name string, prefix helpers.BitPrefix) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			cont = vm.ForceControlData(cont)
			data := cont.GetControlData()
			if mask&1 != 0 {
				data.Save.C[0] = val
			}
			if mask&2 != 0 {
				data.Save.C[1] = val.Copy()
			}
			return state.Stack.PushContinuation(cont)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func BOOLAND() *helpers.SimpleOP {
	return composeCont(1, "BOOLAND", helpers.BytesPrefix(0xED, 0xF0))
}

func COMPOSBOTH() *helpers.SimpleOP {
	return composeCont(3, "COMPOSBOTH", helpers.BytesPrefix(0xED, 0xF2))
}

func ATEXIT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			cont = vm.ForceControlData(cont)
			cont.GetControlData().Save.C[0] = state.Reg.C[0]
			state.Reg.C[0] = cont
			return nil
		},
		Name:      "ATEXIT",
		BitPrefix: helpers.BytesPrefix(0xED, 0xF3),
	}
}

func ATEXITALT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			cont = vm.ForceControlData(cont)
			cont.GetControlData().Save.C[1] = state.Reg.C[1]
			state.Reg.C[1] = cont
			return nil
		},
		Name:      "ATEXITALT",
		BitPrefix: helpers.BytesPrefix(0xED, 0xF4),
	}
}

func SETEXITALT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			cont = vm.ForceControlData(cont)
			data := cont.GetControlData()
			data.Save.C[0] = state.Reg.C[0]
			data.Save.C[1] = state.Reg.C[1]
			state.Reg.C[1] = cont
			return nil
		},
		Name:      "SETEXITALT",
		BitPrefix: helpers.BytesPrefix(0xED, 0xF5),
	}
}

func THENRET() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			cont = vm.ForceControlData(cont)
			cont.GetControlData().Save.C[0] = state.Reg.C[0]
			return state.Stack.PushContinuation(cont)
		},
		Name:      "THENRET",
		BitPrefix: helpers.BytesPrefix(0xED, 0xF6),
	}
}

func THENRETALT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			cont = vm.ForceControlData(cont)
			cont.GetControlData().Save.C[0] = state.Reg.C[1]
			return state.Stack.PushContinuation(cont)
		},
		Name:      "THENRETALT",
		BitPrefix: helpers.BytesPrefix(0xED, 0xF7),
	}
}

func INVERT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			state.Reg.C[0], state.Reg.C[1] = state.Reg.C[1], state.Reg.C[0]
			return nil
		},
		Name:      "INVERT",
		BitPrefix: helpers.BytesPrefix(0xED, 0xF8),
	}
}

func BOOLEVAL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			cc, err := state.ExtractCurrentContinuation(3, -1, -1)
			if err != nil {
				return err
			}
			state.Reg.C[0] = &vm.PushIntContinuation{Int: -1, Next: cc.Copy()}
			state.Reg.C[1] = &vm.PushIntContinuation{Int: 0, Next: cc}
			return state.Jump(cont)
		},
		Name:      "BOOLEVAL",
		BitPrefix: helpers.BytesPrefix(0xED, 0xF9),
	}
}
