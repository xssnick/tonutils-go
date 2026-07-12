package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SAVECTR(0) })
}

// OpSAVECTR is a struct-based opcode: one allocation per executed instruction
// instead of an AdvancedOP carrying per-instance closures.
type OpSAVECTR struct {
	i int
}

func SAVECTR(i int) *OpSAVECTR {
	return &OpSAVECTR{i: i}
}

func (op *OpSAVECTR) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(saveCtrPrefixes...)
}

func (op *OpSAVECTR) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(saveCtrBitPrefix.Bits); err != nil {
		return err
	}

	idx, err := loadControlRegisterIndex(code)
	if err != nil {
		return err
	}
	op.i = idx
	return nil
}

func (op *OpSAVECTR) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreSlice(saveCtrBitPrefix.Data, saveCtrBitPrefix.Bits).
		MustStoreUInt(uint64(op.i), 4)
}

func (op *OpSAVECTR) SerializeText() string {
	return fmt.Sprintf("c%d SAVECTR", op.i)
}

func (op *OpSAVECTR) InstructionBits() int64 {
	return int64(saveCtrBitPrefix.Bits) + 4
}

func (op *OpSAVECTR) Interpret(state *vm.State) error {
	c0 := vm.ForceControlData(cloneContinuation(state.Reg.C[0]))
	data := c0.GetControlData()
	if !defineControlRegister(state, &data.Save, op.i, cloneControlRegisterValue(state.Reg.Get(op.i))) {
		return vmerr.Error(vmerr.CodeTypeCheck)
	}

	state.Reg.C[0] = c0
	return nil
}
