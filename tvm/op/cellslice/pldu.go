package cellslice

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return PLDU(0) })
}

// OpPLDU is a struct-based opcode: one allocation per executed instruction
// instead of an AdvancedOP carrying per-instance closures.
type OpPLDU struct {
	sz uint
}

var plduPrefix = helpers.BytesPrefix(0xD7, 0x0B)

func PLDU(sz uint) *OpPLDU {
	return &OpPLDU{sz: sz}
}

func (op *OpPLDU) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(plduPrefix)
}

func (op *OpPLDU) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(plduPrefix.Bits); err != nil {
		return err
	}
	val, err := code.LoadUInt(8)
	if err != nil {
		return err
	}
	op.sz = uint(val) + 1
	return nil
}

func (op *OpPLDU) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreSlice(plduPrefix.Data, plduPrefix.Bits).
		MustStoreUInt(uint64(op.sz-1), 8)
}

func (op *OpPLDU) SerializeText() string {
	return fmt.Sprintf("%d PLDU", op.sz)
}

func (op *OpPLDU) InstructionBits() int64 {
	return int64(plduPrefix.Bits) + 8
}

func (op *OpPLDU) Interpret(state *vm.State) error {
	s0, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}

	i, err := s0.PreloadBigUInt(op.sz)
	if err != nil {
		return err
	}

	return state.Stack.PushOwnedInt(i)
}
