package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return PUSHCTR(0) },
		func() vm.OP { return POPCTR(0) },
		func() vm.OP { return SETRETCTR(0) },
		func() vm.OP { return SETALTCTR(0) },
		func() vm.OP { return POPSAVECTR(0) },
		func() vm.OP { return SAVEALTCTR(0) },
		func() vm.OP { return SAVEBOTHCTR(0) },
		func() vm.OP { return PUSHCTRX() },
		func() vm.OP { return POPCTRX() },
		func() vm.OP { return SETCONTCTRX() },
	)
}

func validControlRegisterIndex(i int) bool {
	return (i >= 0 && i <= 5) || i == 7
}

func deserializeControlRegisterIndex(dst *int) func(*cell.Slice) error {
	return func(code *cell.Slice) error {
		val, err := code.LoadUInt(4)
		if err != nil {
			return err
		}

		idx := int(val)
		if !validControlRegisterIndex(idx) {
			return vm.ErrCorruptedOpcode
		}

		*dst = idx
		return nil
	}
}

func serializeControlRegisterIndex(src *int) func() *cell.Builder {
	return func() *cell.Builder {
		return cell.BeginCell().MustStoreUInt(uint64(*src), 4)
	}
}

func cloneControlRegisterValue(v any) any {
	switch val := v.(type) {
	case vm.Continuation:
		if val == nil {
			return nil
		}
		return val.Copy()
	case tuple.Tuple:
		return val.Copy()
	default:
		return v
	}
}

func setControlRegister(state *vm.State, idx int, val any) error {
	if !state.Reg.Define(idx, val) {
		return vmerr.Error(vmerr.CodeTypeCheck)
	}
	return nil
}

func PUSHCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			return state.Stack.PushAny(cloneControlRegisterValue(state.Reg.Get(i)))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d PUSH", i)
		},
		BitPrefix:         helpers.SlicePrefix(12, []byte{0xED, 0x40}),
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}

func POPCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			return setControlRegister(state, i, val)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d POP", i)
		},
		BitPrefix:         helpers.SlicePrefix(12, []byte{0xED, 0x50}),
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}

func SETRETCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}

			c0 := vm.ForceControlData(state.Reg.C[0])
			if !c0.GetControlData().Save.Define(i, val) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}
			state.Reg.C[0] = c0
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SETRETCTR", i)
		},
		BitPrefix:         helpers.SlicePrefix(12, []byte{0xED, 0x70}),
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}

func SETALTCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}

			c1 := vm.ForceControlData(state.Reg.C[1])
			if !c1.GetControlData().Save.Define(i, val) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}
			state.Reg.C[1] = c1
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SETALTCTR", i)
		},
		BitPrefix:         helpers.SlicePrefix(12, []byte{0xED, 0x80}),
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}

func POPSAVECTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			if i == 0 {
				if _, ok := val.(vm.Continuation); !ok {
					return vmerr.Error(vmerr.CodeTypeCheck)
				}
			}

			c0 := vm.ForceControlData(state.Reg.C[0])
			if !c0.GetControlData().Save.Define(i, cloneControlRegisterValue(state.Reg.Get(i))) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}

			if i == 0 {
				state.Reg.C[0] = c0
				return setControlRegister(state, i, val)
			}

			if err = setControlRegister(state, i, val); err != nil {
				return err
			}
			state.Reg.C[0] = c0
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d POPSAVE", i)
		},
		BitPrefix:         helpers.SlicePrefix(12, []byte{0xED, 0x90}),
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}

func SAVEALTCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			c1 := vm.ForceControlData(state.Reg.C[1])
			if !c1.GetControlData().Save.Define(i, cloneControlRegisterValue(state.Reg.Get(i))) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}
			state.Reg.C[1] = c1
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SAVEALTCTR", i)
		},
		BitPrefix:         helpers.SlicePrefix(12, []byte{0xED, 0xB0}),
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}

func SAVEBOTHCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			c0 := vm.ForceControlData(state.Reg.C[0])
			c1 := vm.ForceControlData(state.Reg.C[1])
			val := state.Reg.Get(i)

			if !c0.GetControlData().Save.Define(i, cloneControlRegisterValue(val)) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}
			if !c1.GetControlData().Save.Define(i, cloneControlRegisterValue(val)) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}

			state.Reg.C[0] = c0
			state.Reg.C[1] = c1
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SAVEBOTHCTR", i)
		},
		BitPrefix:         helpers.SlicePrefix(12, []byte{0xED, 0xC0}),
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}

func PUSHCTRX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name: "PUSHCTRX",
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopIntRange(0, 16)
			if err != nil {
				return err
			}
			i := int(idx.Int64())
			if !validControlRegisterIndex(i) {
				return vmerr.Error(vmerr.CodeRangeCheck)
			}
			return state.Stack.PushAny(cloneControlRegisterValue(state.Reg.Get(i)))
		},
		BitPrefix: helpers.BytesPrefix(0xED, 0xE0),
	}
}

func POPCTRX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name: "POPCTRX",
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopIntRange(0, 16)
			if err != nil {
				return err
			}
			i := int(idx.Int64())
			if !validControlRegisterIndex(i) {
				return vmerr.Error(vmerr.CodeRangeCheck)
			}
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			return setControlRegister(state, i, val)
		},
		BitPrefix: helpers.BytesPrefix(0xED, 0xE1),
	}
}

func SETCONTCTRX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name: "SETCONTCTRX",
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopIntRange(0, 16)
			if err != nil {
				return err
			}
			i := int(idx.Int64())
			if !validControlRegisterIndex(i) {
				return vmerr.Error(vmerr.CodeRangeCheck)
			}

			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}

			cont = vm.ForceControlData(cont)
			if !cont.GetControlData().Save.Define(i, val) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}
			return state.Stack.PushContinuation(cont)
		},
		BitPrefix: helpers.BytesPrefix(0xED, 0xE2),
	}
}
