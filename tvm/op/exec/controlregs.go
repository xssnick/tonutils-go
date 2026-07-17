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

func deserializeControlRegisterIndex(dst *int) func(*cell.Slice) error {
	return func(code *cell.Slice) error {
		idx, err := loadControlRegisterIndex(code)
		if err != nil {
			return err
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
		return ok && c != nil
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
		return r.C[idx] != nil
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

// OpPUSHCTR is a struct-based opcode: one allocation per executed instruction
// instead of an AdvancedOP carrying per-instance closures.
type OpPUSHCTR struct {
	i int
}

func PUSHCTR(i int) *OpPUSHCTR {
	return &OpPUSHCTR{i: i}
}

func (op *OpPUSHCTR) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(pushCtrPrefixes...)
}

func (op *OpPUSHCTR) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(pushCtrBitPrefix.Bits); err != nil {
		return err
	}

	idx, err := loadControlRegisterIndex(code)
	if err != nil {
		return err
	}
	op.i = idx
	return nil
}

func (op *OpPUSHCTR) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreSlice(pushCtrBitPrefix.Data, pushCtrBitPrefix.Bits).
		MustStoreUInt(uint64(op.i), 4)
}

func (op *OpPUSHCTR) SerializeText() string {
	return fmt.Sprintf("c%d PUSH", op.i)
}

func (op *OpPUSHCTR) InstructionBits() int64 {
	return int64(pushCtrBitPrefix.Bits) + 4
}

func (op *OpPUSHCTR) Interpret(state *vm.State) error {
	if op.i == 4 || op.i == 5 {
		return state.Stack.PushCell(state.Reg.D[op.i-4])
	}

	return state.Stack.PushAny(cloneControlRegisterValue(state.Reg.Get(op.i)))
}

// OpPOPCTR is a struct-based opcode: one allocation per executed instruction
// instead of an AdvancedOP carrying per-instance closures.
type OpPOPCTR struct {
	i int
}

func POPCTR(i int) *OpPOPCTR {
	return &OpPOPCTR{i: i}
}

func (op *OpPOPCTR) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(popCtrPrefixes...)
}

func (op *OpPOPCTR) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(popCtrBitPrefix.Bits); err != nil {
		return err
	}

	idx, err := loadControlRegisterIndex(code)
	if err != nil {
		return err
	}
	op.i = idx
	return nil
}

func (op *OpPOPCTR) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreSlice(popCtrBitPrefix.Data, popCtrBitPrefix.Bits).
		MustStoreUInt(uint64(op.i), 4)
}

func (op *OpPOPCTR) SerializeText() string {
	return fmt.Sprintf("c%d POP", op.i)
}

func (op *OpPOPCTR) InstructionBits() int64 {
	return int64(popCtrBitPrefix.Bits) + 4
}

func (op *OpPOPCTR) Interpret(state *vm.State) error {
	val, err := state.Stack.PopAny()
	if err != nil {
		return err
	}
	return setControlRegister(state, op.i, val)
}

func SETRETCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
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
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SETRETCTR", i)
		},
		BitPrefix:         setRetCtrBitPrefix,
		Prefixes:          setRetCtrPrefixes,
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

			c1 := vm.ForceControlData(cloneContinuation(state.Reg.C[1]))
			data := c1.GetControlData()
			if !defineControlRegister(state, &data.Save, i, cloneControlRegisterValue(val)) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}
			state.Reg.C[1] = c1
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SETALTCTR", i)
		},
		BitPrefix:         setAltCtrBitPrefix,
		Prefixes:          setAltCtrPrefixes,
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
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d POPSAVE", i)
		},
		BitPrefix:         popSaveCtrBitPrefix,
		Prefixes:          popSaveCtrPrefixes,
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}

func SAVEALTCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			c1 := vm.ForceControlData(cloneContinuation(state.Reg.C[1]))
			data := c1.GetControlData()
			if !defineControlRegister(state, &data.Save, i, cloneControlRegisterValue(state.Reg.Get(i))) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}
			state.Reg.C[1] = c1
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SAVEALTCTR", i)
		},
		BitPrefix:         saveAltCtrBitPrefix,
		Prefixes:          saveAltCtrPrefixes,
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}

func SAVEBOTHCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			c0 := vm.ForceControlData(cloneContinuation(state.Reg.C[0]))
			c1 := vm.ForceControlData(cloneContinuation(state.Reg.C[1]))
			val := state.Reg.Get(i)

			c0.GetControlData().Save.Define(i, cloneControlRegisterValue(val))
			c1.GetControlData().Save.Define(i, cloneControlRegisterValue(val))

			state.Reg.C[0] = c0
			state.Reg.C[1] = c1
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SAVEBOTHCTR", i)
		},
		BitPrefix:         saveBothCtrBitPrefix,
		Prefixes:          saveBothCtrPrefixes,
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
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
			return state.Stack.PushContinuation(cont)
		},
		BitPrefix: helpers.BytesPrefix(0xED, 0xE2),
	}
}
