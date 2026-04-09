package cellslice

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type OpSDBEGINSCONST struct {
	value *cell.Slice
	quiet bool
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return SDBEGINSCONST(cell.BeginCell().EndCell().BeginParse(), false) })
}

func SDBEGINSCONST(value *cell.Slice, quiet bool) *OpSDBEGINSCONST {
	if value == nil {
		value = cell.BeginCell().EndCell().BeginParse()
	}
	return &OpSDBEGINSCONST{
		value: value.Copy(),
		quiet: quiet,
	}
}

func paddedBeginsConstBits(actual uint) uint {
	bits := actual + 1
	for bits%8 != 3 {
		bits++
	}
	return bits
}

func encodeBeginsConstPayload(value *cell.Slice, totalBits uint) *cell.Builder {
	b := value.ToBuilder()
	actual := value.BitsLeft()
	if totalBits <= actual {
		return b
	}
	b.MustStoreUInt(1, 1)
	if pad := totalBits - actual - 1; pad > 0 {
		b.MustStoreUInt(0, pad)
	}
	return b
}

func (op *OpSDBEGINSCONST) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(helpers.UIntPrefix(0xD728>>3, 13))
}

func (op *OpSDBEGINSCONST) Deserialize(code *cell.Slice) error {
	if _, err := code.LoadSlice(13); err != nil {
		return err
	}
	arg, err := code.LoadUInt(8)
	if err != nil {
		return err
	}
	op.quiet = (arg & 0x80) != 0
	bits := uint((arg&0x7F)*8 + 3)
	sl, err := code.FetchSubslice(bits, 0)
	if err != nil {
		return err
	}
	sl.RemoveTrailing()
	op.value = sl
	return nil
}

func (op *OpSDBEGINSCONST) Serialize() *cell.Builder {
	value := op.value.Copy()
	totalBits := paddedBeginsConstBits(value.BitsLeft())
	arg := uint64((totalBits - 3) / 8)
	if op.quiet {
		arg |= 0x80
	}
	return cell.BeginCell().
		MustStoreUInt(0xD728>>3, 13).
		MustStoreUInt(arg, 8).
		MustStoreBuilder(encodeBeginsConstPayload(value, totalBits))
}

func (op *OpSDBEGINSCONST) SerializeText() string {
	name := "SDBEGINS"
	if op.quiet {
		name = "SDBEGINSQ"
	}
	return fmt.Sprintf("%s %s", name, op.value.WithoutObserver().MustToCell().DumpBits())
}

func (op *OpSDBEGINSCONST) InstructionBits() int64 {
	return 21
}

func (op *OpSDBEGINSCONST) Interpret(state *vm.State) error {
	needle := op.value.Copy()
	cs, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}
	if !cs.HasPrefix(needle) {
		if !op.quiet {
			return vmerr.Error(vmerr.CodeCellUnderflow, "slice does not begin with expected data bits")
		}
		if err = state.Stack.PushSlice(cs); err != nil {
			return err
		}
		return state.Stack.PushBool(false)
	}
	if err = cs.Advance(needle.BitsLeft()); err != nil {
		return vmerr.Error(vmerr.CodeCellUnderflow)
	}
	if err = state.Stack.PushSlice(cs); err != nil {
		return err
	}
	if op.quiet {
		return state.Stack.PushBool(true)
	}
	return nil
}
