package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type OpPUSHCONT struct {
	helpers.Prefixed
	cont *cell.Cell
	typ  string
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHCONT(nil) })
}

var (
	pushContPrefixed = helpers.NewPrefixed(
		helpers.UIntPrefix(0x9, 4),
		helpers.UIntPrefix(0x47, 7),
		helpers.UIntPrefix(0x8A, 8),
	)
	pushRefContPrefixed = helpers.NewPrefixed(helpers.UIntPrefix(0x8A, 8))
)

func PUSHCONT(cont *cell.Cell) *OpPUSHCONT {
	return &OpPUSHCONT{
		Prefixed: pushContPrefixed,
		cont:     cont,
	}
}

func PUSHREFCONT(cont *cell.Cell) *OpPUSHCONT {
	return &OpPUSHCONT{
		Prefixed: pushRefContPrefixed,
		cont:     cont,
		typ:      "REF",
	}
}

func (op *OpPUSHCONT) Deserialize(code *cell.Slice) error {
	prefix, err := code.LoadUInt(4)
	if err != nil {
		return err
	}

	op.typ = "SMALL"

	if prefix != 0x9 {
		prefix2, err := code.LoadUInt(3)
		if err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}

		op.typ = "BIG"

		prefix = (prefix << 3) | prefix2
		if prefix != 0x47 {
			prefix3, err := code.LoadUInt(1)
			if err != nil {
				return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
			}

			op.typ = "REF"

			prefix = (prefix << 1) | prefix3
			if prefix != 0x8A {
				return vm.ErrCorruptedOpcode
			}
		}
	}

	switch op.typ {
	case "SMALL":
		szBytes, err := code.LoadUInt(4)
		if err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}

		data, err := code.LoadSlice(uint(szBytes * 8))
		if err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}

		op.cont = cell.BeginCell().MustStoreSlice(data, uint(szBytes*8)).EndCell()
		return nil
	case "BIG":
		refsNum, err := code.LoadUInt(2)
		if err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}

		szBytes, err := code.LoadUInt(7)
		if err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}

		data, err := code.LoadSlice(uint(szBytes * 8))
		if err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}

		cn := cell.BeginCell().MustStoreSlice(data, uint(szBytes*8))

		for i := 0; i < int(refsNum); i++ {
			ref, err := code.PeekRefCell()
			if err != nil {
				return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
			}
			if err = code.SkipBitsAndRefs(0, 1); err != nil {
				return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
			}

			err = cn.StoreRef(ref)
			if err != nil {
				return err
			}
		}

		op.cont = cn.EndCell()
		return nil
	case "REF":
		ref, err := code.PeekRefCell()
		if err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}
		if err = code.SkipBitsAndRefs(0, 1); err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}

		op.cont = ref
		return nil
	}

	return vm.ErrCorruptedOpcode
}

func (op *OpPUSHCONT) Serialize() *cell.Builder {
	var b *cell.Builder
	switch {
	case op.typ == "REF" || op.cont.BitsSize()%8 != 0:
		op.typ = "REF"
		b = cell.BeginCell().
			MustStoreUInt(0x8A, 8).MustStoreRef(op.cont)
	case op.cont.RefsNum() == 0 && op.cont.BitsSize() <= 16*8:
		op.typ = "SMALL"

		sz := uint64(op.cont.BitsSize() / 8)
		codeSlice, err := op.cont.BeginParse()
		if err != nil {
			panic(err)
		}

		b = cell.BeginCell().
			MustStoreUInt(9, 4).
			MustStoreUInt(sz, 4).
			MustStoreSlice(codeSlice.MustLoadSlice(op.cont.BitsSize()), uint(sz*8))
	case op.cont.RefsNum() <= 3:
		op.typ = "BIG"

		sz := uint64(op.cont.BitsSize() / 8)

		codeSlice, err := op.cont.BeginParse()
		if err != nil {
			panic(err)
		}

		b = cell.BeginCell().
			MustStoreUInt(0x47, 7). // 0x8E >> 1 = 0x47
			MustStoreUInt(uint64(op.cont.RefsNum()), 2).
			MustStoreUInt(sz, 7).
			MustStoreSlice(codeSlice.MustLoadSlice(op.cont.BitsSize()), uint(sz*8))

		for codeSlice.RefsNum() > 0 {
			b.MustStoreRef(codeSlice.MustLoadRef().MustToCell())
		}
	default:
		panic("PUSHCONT cannot encode continuation with more than 3 refs; use PUSHREFCONT")
	}

	return b
}

func (op *OpPUSHCONT) SerializeText() string {
	str := "???"
	if op.cont != nil {
		str = op.cont.Dump()
	}
	if op.typ == "REF" {
		return fmt.Sprintf("<%s> PUSHREFCONT", str)
	}
	return fmt.Sprintf("<%s> %s PUSHCONT", str, op.typ)
}

func (op *OpPUSHCONT) InstructionBits() int64 {
	switch op.typ {
	case "SMALL":
		return 8
	case "BIG":
		return 16
	case "REF":
		return 8
	default:
		return 8
	}
}

func (op *OpPUSHCONT) Interpret(state *vm.State) error {
	var code *cell.Slice
	if op.typ == "REF" {
		if err := state.Cells.RegisterCellLoad(op.cont); err != nil {
			return err
		}
		var err error
		code, err = state.Cells.BeginParseAlreadyLoadedRaw(op.cont)
		if err != nil {
			return err
		}
	} else {
		var err error
		code, err = op.cont.BeginParse()
		if err != nil {
			return err
		}
	}
	return state.Stack.PushContinuation(&vm.OrdinaryContinuation{Code: code, Data: vm.ControlData{
		NumArgs: vm.ControlDataAllArgs,
		CP:      state.CP,
	}})
}
