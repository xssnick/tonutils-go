package cellslice

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return STU(0) })
}

// OpSTU is a struct-based opcode: one allocation per executed instruction
// instead of an AdvancedOP carrying per-instance closures.
type OpSTU struct {
	sz uint
}

var stuPrefix = helpers.BytesPrefix(0xCB)

func STU(sz uint) *OpSTU {
	return &OpSTU{sz: sz}
}

func (op *OpSTU) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(stuPrefix)
}

func (op *OpSTU) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(stuPrefix.Bits); err != nil {
		return err
	}
	val, err := code.LoadUInt(8)
	if err != nil {
		return err
	}
	op.sz = uint(val) + 1
	return nil
}

func (op *OpSTU) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreSlice(stuPrefix.Data, stuPrefix.Bits).
		MustStoreUInt(uint64(op.sz-1), 8)
}

func (op *OpSTU) SerializeText() string {
	return fmt.Sprintf("%d STU", op.sz)
}

func (op *OpSTU) InstructionBits() int64 {
	return int64(stuPrefix.Bits) + 8
}

func (op *OpSTU) Interpret(state *vm.State) error {
	if err := checkStackDepth(state, 2); err != nil {
		return err
	}

	b0, err := state.Stack.PopBuilder()
	if err != nil {
		return err
	}

	i1, err := state.Stack.PopIntRead()
	if err != nil {
		return err
	}

	if !b0.CanExtendBy(op.sz, 0) {
		return vmerr.Error(vmerr.CodeCellOverflow)
	}
	if err := b0.StoreBigUInt(i1, op.sz); err != nil {
		return err
	}
	return state.Stack.PushOwnedBuilder(b0)
}
