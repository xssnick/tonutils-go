package cellslice

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return ENDCST() },
		func() vm.OP { return STREFCONST(nil) },
		func() vm.OP { return STSLICECONST(cell.BeginCell().EndCell().BeginParse()) },
		func() vm.OP { return BREMBITS() },
		func() vm.OP { return BREMREFS() },
		func() vm.OP { return BREMBITREFS() },
	)
}

type OpSTREFCONST struct {
	refs []*cell.Cell
}

func STREFCONST(ref *cell.Cell) *OpSTREFCONST {
	return &OpSTREFCONST{refs: []*cell.Cell{ref}}
}

func STREF2CONST(first, second *cell.Cell) *OpSTREFCONST {
	return &OpSTREFCONST{refs: []*cell.Cell{first, second}}
}

func (op *OpSTREFCONST) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(helpers.UIntPrefix(0xCF20>>1, 15))
}

func (op *OpSTREFCONST) Deserialize(code *cell.Slice) error {
	if _, err := code.LoadUInt(15); err != nil {
		return err
	}
	v, err := code.LoadUInt(1)
	if err != nil {
		return err
	}
	count := int(v) + 1
	refs := make([]*cell.Cell, count)
	for i := 0; i < count; i++ {
		ref, err := code.PeekRefCell()
		if err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, "no references left for a STREFCONST instruction")
		}
		if err = code.AdvanceExt(0, 1); err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, "no references left for a STREFCONST instruction")
		}
		refs[i] = ref
	}
	op.refs = refs
	return nil
}

func (op *OpSTREFCONST) Serialize() *cell.Builder {
	if len(op.refs) == 0 || len(op.refs) > 2 {
		panic("STREFCONST expects 1 or 2 references")
	}
	b := cell.BeginCell().
		MustStoreUInt(0xCF20>>1, 15).
		MustStoreUInt(uint64(len(op.refs)-1), 1)
	for _, ref := range op.refs {
		b.MustStoreRef(ref)
	}
	return b
}

func (op *OpSTREFCONST) SerializeText() string {
	if len(op.refs) == 2 {
		return "STREF2CONST"
	}
	return "STREFCONST"
}

func (op *OpSTREFCONST) InstructionBits() int64 {
	return 16
}

func (op *OpSTREFCONST) Interpret(state *vm.State) error {
	builder, err := state.Stack.PopBuilder()
	if err != nil {
		return err
	}
	if !builder.CanExtendBy(0, uint(len(op.refs))) {
		return vmerr.Error(vmerr.CodeCellOverflow)
	}
	for _, ref := range op.refs {
		if err = builder.StoreRef(ref); err != nil {
			return vmerr.Error(vmerr.CodeCellOverflow)
		}
	}
	return state.Stack.PushBuilder(builder)
}

type OpSTSLICECONST struct {
	value *cell.Slice
}

func STSLICECONST(value *cell.Slice) *OpSTSLICECONST {
	if value == nil {
		value = cell.BeginCell().EndCell().BeginParse()
	}
	return &OpSTSLICECONST{value: value.Copy()}
}

func paddedConstStoreSliceBits(actual uint) uint {
	bits := actual + 1
	for bits%8 != 2 {
		bits++
	}
	return bits
}

func encodeConstStoreSlicePayload(value *cell.Slice, totalBits uint) *cell.Builder {
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

func (op *OpSTSLICECONST) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(helpers.UIntPrefix(0xCF80>>7, 9))
}

func (op *OpSTSLICECONST) Deserialize(code *cell.Slice) error {
	if _, err := code.LoadUInt(9); err != nil {
		return err
	}
	arg, err := code.LoadUInt(5)
	if err != nil {
		return err
	}
	refs := int((arg >> 3) & 0x3)
	bits := uint((arg&0x7)*8 + 2)
	sl, err := code.FetchSubslice(bits, refs)
	if err != nil {
		return err
	}
	sl.RemoveTrailing()
	op.value = sl
	return nil
}

func (op *OpSTSLICECONST) Serialize() *cell.Builder {
	value := op.value.Copy()
	totalBits := paddedConstStoreSliceBits(value.BitsLeft())
	if value.RefsNum() > 3 {
		panic("STSLICECONST supports at most 3 references")
	}
	if totalBits < 2 || totalBits > 58 {
		panic("STSLICECONST payload size is out of range")
	}
	arg := uint64((value.RefsNum() << 3) | int((totalBits-2)/8))
	return cell.BeginCell().
		MustStoreUInt(0xCF80>>7, 9).
		MustStoreUInt(arg, 5).
		MustStoreBuilder(encodeConstStoreSlicePayload(value, totalBits))
}

func (op *OpSTSLICECONST) SerializeText() string {
	return fmt.Sprintf("STSLICECONST %s", op.value.WithoutObserver().MustToCell().DumpBits())
}

func (op *OpSTSLICECONST) InstructionBits() int64 {
	return 14
}

func (op *OpSTSLICECONST) Interpret(state *vm.State) error {
	builder, err := state.Stack.PopBuilder()
	if err != nil {
		return err
	}
	if !builder.CanExtendBy(op.value.BitsLeft(), uint(op.value.RefsNum())) {
		return vmerr.Error(vmerr.CodeCellOverflow)
	}
	if err = builder.StoreBuilder(op.value.ToBuilder()); err != nil {
		return vmerr.Error(vmerr.CodeCellOverflow)
	}
	return state.Stack.PushBuilder(builder)
}

func ENDCST() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			src, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(0, 1) {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			if err = dst.StoreRef(src.EndCell()); err != nil {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			return state.Stack.PushBuilder(dst)
		},
		Name:      "ENDCST",
		BitPrefix: helpers.BytesPrefix(0xCD),
	}
}

func BREMBITS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			return pushBuilderInt(state, int64(builder.BitsLeft()))
		},
		Name:      "BREMBITS",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x35),
	}
}

func BREMREFS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			return pushBuilderInt(state, int64(builder.RefsLeft()))
		},
		Name:      "BREMREFS",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x36),
	}
}

func BREMBITREFS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if err = pushBuilderInt(state, int64(builder.BitsLeft())); err != nil {
				return err
			}
			return pushBuilderInt(state, int64(builder.RefsLeft()))
		},
		Name:      "BREMBITREFS",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x37),
	}
}
