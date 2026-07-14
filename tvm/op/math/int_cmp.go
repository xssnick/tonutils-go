package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// int cmp result masks: bit 0 set means "true when x < value",
// bit 1 - "when x == value", bit 2 - "when x > value".
const (
	intCmpMaskLess    = 0b001
	intCmpMaskEqual   = 0b010
	intCmpMaskGreater = 0b100
)

// OpIntCmp is the struct-based form of the <int8-argument> comparison opcodes
// (LESSINT/EQINT/GTINT/NEQINT): one allocation per executed instruction
// instead of an AdvancedOP carrying per-instance closures. When fixed is set,
// the instance covers a single full 16-bit opcode with a baked-in value, its
// Deserialize does not mutate the op, so it is shared via the dispatch table.
type OpIntCmp struct {
	name   string
	prefix byte
	mask   uint8
	value  int8
	fixed  bool
}

func fixedIntCmpVariant(op *OpIntCmp) *OpIntCmp {
	op.fixed = true
	return op
}

func (op *OpIntCmp) Reusable() bool {
	return op.fixed
}

func (op *OpIntCmp) GetPrefixes() []*cell.Slice {
	if op.fixed {
		return helpers.PrefixSlices(helpers.UIntPrefix(uint64(op.prefix)<<8|uint64(uint8(op.value)), 16))
	}
	return helpers.PrefixSlices(helpers.BytesPrefix(op.prefix))
}

func (op *OpIntCmp) Deserialize(code *cell.Slice) error {
	if op.fixed {
		// value is baked into the full opcode prefix
		return code.SkipBits(16)
	}

	if err := code.SkipBits(8); err != nil {
		return err
	}
	val, err := code.LoadInt(8)
	if err != nil {
		return err
	}
	op.value = int8(val)
	return nil
}

func (op *OpIntCmp) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreUInt(uint64(op.prefix), 8).
		MustStoreInt(int64(op.value), 8)
}

func (op *OpIntCmp) SerializeText() string {
	return fmt.Sprintf("%d %s", op.value, op.name)
}

func (op *OpIntCmp) InstructionBits() int64 {
	return 16
}

func (op *OpIntCmp) Interpret(state *vm.State) error {
	i0, err := state.Stack.PopIntRead()
	if err != nil {
		return err
	}
	if i0 == nil {
		return pushNaNOrOverflow(state, false)
	}

	bit := uint8(1) << (compareBigIntInt64(i0, int64(op.value)) + 1)
	return state.Stack.PushBool(op.mask&bit != 0)
}
