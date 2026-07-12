package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SETCONTCTR(0) })
}

// OpSETCONTCTR is a struct-based opcode: one allocation per executed
// instruction instead of an AdvancedOP carrying per-instance closures.
type OpSETCONTCTR struct {
	i int
}

func SETCONTCTR(i int) *OpSETCONTCTR {
	return &OpSETCONTCTR{i: i}
}

func (op *OpSETCONTCTR) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(setContCtrPrefixes...)
}

func (op *OpSETCONTCTR) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(setContCtrBitPrefix.Bits); err != nil {
		return err
	}

	idx, err := loadControlRegisterIndex(code)
	if err != nil {
		return err
	}
	op.i = idx
	return nil
}

func (op *OpSETCONTCTR) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreSlice(setContCtrBitPrefix.Data, setContCtrBitPrefix.Bits).
		MustStoreUInt(uint64(op.i), 4)
}

func (op *OpSETCONTCTR) SerializeText() string {
	return fmt.Sprintf("c%d SETCONTCTR", op.i)
}

func (op *OpSETCONTCTR) InstructionBits() int64 {
	return int64(setContCtrBitPrefix.Bits) + 4
}

func (op *OpSETCONTCTR) Interpret(state *vm.State) error {
	if err := checkStackDepth(state, 2); err != nil {
		return err
	}

	cont0, err := state.Stack.PopContinuation()
	if err != nil {
		return err
	}

	v1, err := state.Stack.PopAny()
	if err != nil {
		return err
	}

	cont0 = vm.ForceControlData(cont0)
	data := cont0.GetControlData()
	if !defineControlRegister(state, &data.Save, op.i, cloneControlRegisterValue(v1)) {
		return vmerr.Error(vmerr.CodeTypeCheck)
	}

	return state.Stack.PushContinuation(cont0)
}
