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
		func() vm.OP { return PUSHCTRX() },
		func() vm.OP { return POPCTRX() },
		func() vm.OP { return SETCONTCTRX() },
	)
	vm.ArgList = append(vm.ArgList,
		pushCtrOp,
		popCtrOp,
		setRetCtrOp,
		setAltCtrOp,
		popSaveCtrOp,
		saveAltCtrOp,
		saveBothCtrOp,
	)
}

func validControlRegisterIndex(i int) bool {
	return (i >= 0 && i <= 5) || i == 7
}

var controlRegisterPrefixIndexes = [...]uint64{0, 1, 2, 3, 4, 5, 7}

func controlRegisterPrefixes(base uint64) []helpers.BitPrefix {
	prefixes := make([]helpers.BitPrefix, len(controlRegisterPrefixIndexes))
	for i, idx := range controlRegisterPrefixIndexes {
		prefixes[i] = helpers.UIntPrefix(base|idx, 16)
	}
	return prefixes
}

// constant per-op prefixes, computed once instead of on every decode
var (
	pushCtrBitPrefix     = helpers.SlicePrefix(12, []byte{0xED, 0x40})
	pushCtrPrefixes      = controlRegisterPrefixes(0xED40)
	popCtrBitPrefix      = helpers.SlicePrefix(12, []byte{0xED, 0x50})
	popCtrPrefixes       = controlRegisterPrefixes(0xED50)
	setContCtrBitPrefix  = helpers.SlicePrefix(12, []byte{0xED, 0x60})
	setContCtrPrefixes   = controlRegisterPrefixes(0xED60)
	setRetCtrBitPrefix   = helpers.SlicePrefix(12, []byte{0xED, 0x70})
	setRetCtrPrefixes    = controlRegisterPrefixes(0xED70)
	setAltCtrBitPrefix   = helpers.SlicePrefix(12, []byte{0xED, 0x80})
	setAltCtrPrefixes    = controlRegisterPrefixes(0xED80)
	popSaveCtrBitPrefix  = helpers.SlicePrefix(12, []byte{0xED, 0x90})
	popSaveCtrPrefixes   = controlRegisterPrefixes(0xED90)
	saveCtrBitPrefix     = helpers.SlicePrefix(12, []byte{0xED, 0xA0})
	saveCtrPrefixes      = controlRegisterPrefixes(0xEDA0)
	saveAltCtrBitPrefix  = helpers.SlicePrefix(12, []byte{0xED, 0xB0})
	saveAltCtrPrefixes   = controlRegisterPrefixes(0xEDB0)
	saveBothCtrBitPrefix = helpers.SlicePrefix(12, []byte{0xED, 0xC0})
	saveBothCtrPrefixes  = controlRegisterPrefixes(0xEDC0)
)

func loadControlRegisterIndex(code *cell.Slice) (int, error) {
	val, err := code.LoadUInt(4)
	if err != nil {
		return 0, err
	}

	idx := int(val)
	if !validControlRegisterIndex(idx) {
		return 0, vm.ErrCorruptedOpcode
	}
	return idx, nil
}

// newControlRegisterOp builds one of the ED4x..EDCx opcodes. Their registered
// prefixes are 16 bits wide because the register index is part of them — only
// the seven defined indexes are dispatched at all — while the instruction
// itself is a 12-bit opcode followed by that index as a 4-bit operand.
func newControlRegisterOp(
	prefix helpers.BitPrefix,
	prefixes []helpers.BitPrefix,
	name string,
	action func(state *vm.State, i int) error,
) *helpers.ArgOP {
	return helpers.NewArgOP(&helpers.ArgOP{
		Prefixed: helpers.NewPrefixed(prefixes...),
		Action: func(state *vm.State, args uint64) error {
			return action(state, int(args))
		},
		Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
			if err := code.SkipBits(prefix.Bits); err != nil {
				return 0, err
			}
			idx, err := loadControlRegisterIndex(code)
			if err != nil {
				return 0, err
			}
			return uint64(idx), nil
		},
		Serializer: func(args uint64) *cell.Builder {
			return cell.BeginCell().
				MustStoreSlice(prefix.Data, prefix.Bits).
				MustStoreUInt(args, 4)
		},
		Name: func(args uint64) string {
			return fmt.Sprintf("c%d %s", args, name)
		},
	})
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
	if !state.Reg.Set(idx, val) {
		return vmerr.Error(vmerr.CodeTypeCheck)
	}
	return nil
}

func controlRegisterValueHasType(idx int, val any) bool {
	if idx < 0 {
		return false
	}
	if idx < 4 {
		c, ok := val.(vm.Continuation)
		return ok && !vm.IsNullContinuation(c)
	}
	if idx < 6 {
		c, ok := val.(*cell.Cell)
		return ok && c != nil
	}
	if idx == 7 {
		c, ok := val.(tuple.Tuple)
		return ok && !c.IsNull()
	}
	return false
}

func controlRegisterSlotFilled(r *vm.Register, idx int) bool {
	if idx < 0 {
		return false
	}
	if idx < 4 {
		return !vm.IsNullContinuation(r.C[idx])
	}
	if idx < 6 {
		return r.D[idx-4] != nil
	}
	if idx == 7 {
		return !r.C7.IsNull()
	}
	return false
}

func defineControlRegister(state *vm.State, r *vm.Register, idx int, val any) bool {
	if r.Define(idx, val) {
		return true
	}
	return state.GlobalVersion >= 14 && controlRegisterSlotFilled(r, idx) && controlRegisterValueHasType(idx, val)
}

var pushCtrOp = newControlRegisterOp(pushCtrBitPrefix, pushCtrPrefixes, "PUSH", func(state *vm.State, i int) error {
	if i == 4 || i == 5 {
		return state.Stack.PushCell(state.Reg.D[i-4])
	}

	return state.Stack.PushAny(cloneControlRegisterValue(state.Reg.Get(i)))
})

var popCtrOp = newControlRegisterOp(popCtrBitPrefix, popCtrPrefixes, "POP", func(state *vm.State, i int) error {
	val, err := state.Stack.PopAny()
	if err != nil {
		return err
	}
	return setControlRegister(state, i, val)
})

var setRetCtrOp = newControlRegisterOp(setRetCtrBitPrefix, setRetCtrPrefixes, "SETRETCTR", func(state *vm.State, i int) error {
	val, err := state.Stack.PopAny()
	if err != nil {
		return err
	}

	c0 := vm.ForceControlData(cloneContinuation(state.Reg.C[0]))
	data := c0.GetControlData()
	if !defineControlRegister(state, &data.Save, i, cloneControlRegisterValue(val)) {
		return vmerr.Error(vmerr.CodeTypeCheck)
	}
	state.Reg.C[0] = c0
	return nil
})

var setAltCtrOp = newControlRegisterOp(setAltCtrBitPrefix, setAltCtrPrefixes, "SETALTCTR", func(state *vm.State, i int) error {
	val, err := state.Stack.PopAny()
	if err != nil {
		return err
	}

	c1 := vm.ForceControlData(cloneContinuation(state.Reg.C[1]))
	data := c1.GetControlData()
	if !defineControlRegister(state, &data.Save, i, cloneControlRegisterValue(val)) {
		return vmerr.Error(vmerr.CodeTypeCheck)
	}
	state.Reg.C[1] = c1
	return nil
})

var popSaveCtrOp = newControlRegisterOp(popSaveCtrBitPrefix, popSaveCtrPrefixes, "POPSAVE", func(state *vm.State, i int) error {
	val, err := state.Stack.PopAny()
	if err != nil {
		return err
	}
	if i == 0 {
		if _, ok := val.(vm.Continuation); !ok {
			return vmerr.Error(vmerr.CodeTypeCheck)
		}
	}

	c0 := vm.ForceControlData(cloneContinuation(state.Reg.C[0]))
	c0.GetControlData().Save.Define(i, cloneControlRegisterValue(state.Reg.Get(i)))

	if i == 0 {
		state.Reg.C[0] = c0
		return setControlRegister(state, i, val)
	}

	if err = setControlRegister(state, i, val); err != nil {
		return err
	}
	state.Reg.C[0] = c0
	return nil
})

var saveAltCtrOp = newControlRegisterOp(saveAltCtrBitPrefix, saveAltCtrPrefixes, "SAVEALTCTR", func(state *vm.State, i int) error {
	c1 := vm.ForceControlData(cloneContinuation(state.Reg.C[1]))
	data := c1.GetControlData()
	if !defineControlRegister(state, &data.Save, i, cloneControlRegisterValue(state.Reg.Get(i))) {
		return vmerr.Error(vmerr.CodeTypeCheck)
	}
	state.Reg.C[1] = c1
	return nil
})

var saveBothCtrOp = newControlRegisterOp(saveBothCtrBitPrefix, saveBothCtrPrefixes, "SAVEBOTHCTR", func(state *vm.State, i int) error {
	c0 := vm.ForceControlData(cloneContinuation(state.Reg.C[0]))
	c1 := vm.ForceControlData(cloneContinuation(state.Reg.C[1]))
	val := state.Reg.Get(i)

	c0.GetControlData().Save.Define(i, cloneControlRegisterValue(val))
	c1.GetControlData().Save.Define(i, cloneControlRegisterValue(val))

	state.Reg.C[0] = c0
	state.Reg.C[1] = c1
	return nil
})

func PUSHCTR(i int) vm.OP {
	return vm.Bind(pushCtrOp, uint64(i))
}

func POPCTR(i int) vm.OP {
	return vm.Bind(popCtrOp, uint64(i))
}

func SETRETCTR(i int) vm.OP {
	return vm.Bind(setRetCtrOp, uint64(i))
}

func SETALTCTR(i int) vm.OP {
	return vm.Bind(setAltCtrOp, uint64(i))
}

func POPSAVECTR(i int) vm.OP {
	return vm.Bind(popSaveCtrOp, uint64(i))
}

func SAVEALTCTR(i int) vm.OP {
	return vm.Bind(saveAltCtrOp, uint64(i))
}

func SAVEBOTHCTR(i int) vm.OP {
	return vm.Bind(saveBothCtrOp, uint64(i))
}

func PUSHCTRX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name: "PUSHCTRX",
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopIntRangeInt64(0, 16)
			if err != nil {
				return err
			}
			i := int(idx)
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
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			idx, err := state.Stack.PopIntRangeInt64(0, 16)
			if err != nil {
				return err
			}
			i := int(idx)
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
			if state.Stack.Len() < 3 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			idx, err := state.Stack.PopIntRangeInt64(0, 16)
			if err != nil {
				return err
			}
			i := int(idx)
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
			data := cont.GetControlData()
			if !defineControlRegister(state, &data.Save, i, cloneControlRegisterValue(val)) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}
			return state.Stack.PushOwnedContinuation(cont)
		},
		BitPrefix: helpers.BytesPrefix(0xED, 0xE2),
	}
}
