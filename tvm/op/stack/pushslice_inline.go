package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpPUSHSLICEINLINE struct {
	helpers.Prefixed
	value *cell.Slice
	form  string
}

func paddedInlineBits(actual uint, mod uint) uint {
	bits := actual + 1
	for bits%8 != mod {
		bits++
	}
	return bits
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHSLICEINLINE(cell.BeginCell().EndCell().BeginParse()) })
}

func PUSHSLICEINLINE(value *cell.Slice) *OpPUSHSLICEINLINE {
	op := &OpPUSHSLICEINLINE{
		Prefixed: helpers.NewPrefixed(
			helpers.UIntPrefix(0x8B, 8),
			helpers.UIntPrefix(0x8C, 8),
			helpers.UIntPrefix(0x8D, 8),
		),
		value: value.Copy(),
		form:  "LONG",
	}
	op.selectForm()
	return op
}

func (op *OpPUSHSLICEINLINE) selectForm() {
	if op.value == nil {
		op.form = "LONG"
		return
	}

	bits := op.value.BitsLeft()
	refs := op.value.RefsNum()
	shortBits := paddedInlineBits(bits, 4)
	refBits := paddedInlineBits(bits, 1)
	longBits := paddedInlineBits(bits, 6)

	switch {
	case refs == 0 && shortBits <= 15*8+4:
		op.form = "SHORT"
	case refs >= 1 && refs <= 4 && refBits <= 31*8+1:
		op.form = "REF"
	case refs <= 4 && longBits <= 127*8+6:
		op.form = "LONG"
	default:
		op.form = "LONG"
	}
}

func encodeInlineSlicePayload(value *cell.Slice, totalBits uint) *cell.Builder {
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

func (op *OpPUSHSLICEINLINE) Deserialize(code *cell.Slice) error {
	prefix, err := code.LoadUInt(8)
	if err != nil {
		return err
	}

	var bits uint
	var refs int

	switch prefix {
	case 0x8B:
		op.form = "SHORT"
		arg, err := code.LoadUInt(4)
		if err != nil {
			return err
		}
		bits = uint(arg*8 + 4)
	case 0x8C:
		op.form = "REF"
		arg, err := code.LoadUInt(7)
		if err != nil {
			return err
		}
		refs = int((arg>>5)&0x3) + 1
		bits = uint((arg&0x1F)*8 + 1)
	case 0x8D:
		op.form = "LONG"
		arg, err := code.LoadUInt(10)
		if err != nil {
			return err
		}
		refs = int((arg >> 7) & 0x7)
		if refs > 4 {
			return vm.ErrCorruptedOpcode
		}
		bits = uint((arg&0x7F)*8 + 6)
	default:
		return vm.ErrCorruptedOpcode
	}

	slice, err := code.FetchSubslice(bits, refs)
	if err != nil {
		return err
	}
	slice.RemoveTrailing()
	op.value = slice
	return nil
}

func (op *OpPUSHSLICEINLINE) Serialize() *cell.Builder {
	op.selectForm()
	value := op.value.Copy()
	actualBits := value.BitsLeft()

	switch op.form {
	case "SHORT":
		totalBits := paddedInlineBits(actualBits, 4)
		arg := uint64((totalBits - 4) / 8)
		return cell.BeginCell().
			MustStoreUInt(0x8B, 8).
			MustStoreUInt(arg, 4).
			MustStoreBuilder(encodeInlineSlicePayload(value, totalBits))
	case "REF":
		totalBits := paddedInlineBits(actualBits, 1)
		arg := uint64(((value.RefsNum() - 1) << 5) | int((totalBits-1)/8))
		return cell.BeginCell().
			MustStoreUInt(0x8C, 8).
			MustStoreUInt(arg, 7).
			MustStoreBuilder(encodeInlineSlicePayload(value, totalBits))
	default:
		totalBits := paddedInlineBits(actualBits, 6)
		arg := uint64((value.RefsNum() << 7) | int((totalBits-6)/8))
		return cell.BeginCell().
			MustStoreUInt(0x8D, 8).
			MustStoreUInt(arg, 10).
			MustStoreBuilder(encodeInlineSlicePayload(value, totalBits))
	}
}

func (op *OpPUSHSLICEINLINE) SerializeText() string {
	str := "???"
	if op.value != nil {
		str = op.value.WithoutObserver().MustToCell().Dump()
	}
	return fmt.Sprintf("%s PUSHSLICE", str)
}

func (op *OpPUSHSLICEINLINE) InstructionBits() int64 {
	switch op.form {
	case "SHORT":
		return 12
	case "REF":
		return 15
	default:
		return 18
	}
}

func (op *OpPUSHSLICEINLINE) Interpret(state *vm.State) error {
	return state.Stack.PushSlice(op.value)
}
