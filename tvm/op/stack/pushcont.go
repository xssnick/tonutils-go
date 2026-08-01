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
	cont      *cell.Cell
	contTrace *cell.Trace
	// window is the decoded SMALL/BIG continuation body as a sub-slice of the
	// original code cell; no cell is materialized on the hot decode path.
	window cell.Slice
	typ    string
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
	op := &OpPUSHCONT{
		Prefixed: pushContPrefixed,
		cont:     cont,
	}
	if cont != nil {
		op.contTrace = cont.Trace()
	}
	return op
}

func PUSHREFCONT(cont *cell.Cell) *OpPUSHCONT {
	op := &OpPUSHCONT{
		Prefixed: pushRefContPrefixed,
		cont:     cont,
		typ:      "REF",
	}
	if cont != nil {
		op.contTrace = cont.Trace()
	}
	return op
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

		if err = code.FetchSubsliceInto(&op.window, uint(szBytes*8), 0); err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}

		// the trace is stripped so the continuation code behaves exactly like
		// the previously materialized cell did: nil trace here, the gas trace
		// is re-attached on every jump
		op.window.SetTrace(nil)
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

		if err = code.FetchSubsliceInto(&op.window, uint(szBytes*8), int(refsNum)); err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}

		op.window.SetTrace(nil)
		return nil
	case "REF":
		ref, trace, err := code.PeekRefCellAtWithTrace(0)
		if err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}
		if err = code.SkipBitsAndRefs(0, 1); err != nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}

		op.cont = ref
		op.contTrace = trace
		return nil
	}

	return vm.ErrCorruptedOpcode
}

// contCell returns the continuation body as a cell, materializing it from the
// decoded window when needed. Only the cold assembler/trace paths use it; the
// execution path works on the window directly.
func (op *OpPUSHCONT) contCell() *cell.Cell {
	if op.cont == nil && op.window.BaseCell() != nil {
		return op.window.MustToCell()
	}
	return op.cont
}

func (op *OpPUSHCONT) Serialize() *cell.Builder {
	cont := op.contCell()

	var b *cell.Builder
	switch {
	case op.typ == "REF" || cont.BitsSize()%8 != 0 || cont.BitsSize() > 127*8 || cont.RefsNum() > 3:
		op.typ = "REF"
		b = cell.BeginCell().
			MustStoreUInt(0x8A, 8).MustStoreRef(cont)
	case cont.RefsNum() == 0 && cont.BitsSize() <= 15*8:
		op.typ = "SMALL"

		sz := uint64(cont.BitsSize() / 8)
		codeSlice, err := cont.BeginParse()
		if err != nil {
			panic(err)
		}

		b = cell.BeginCell().
			MustStoreUInt(9, 4).
			MustStoreUInt(sz, 4).
			MustStoreSlice(codeSlice.MustLoadSlice(cont.BitsSize()), uint(sz*8))
	case cont.RefsNum() <= 3:
		op.typ = "BIG"

		sz := uint64(cont.BitsSize() / 8)

		codeSlice, err := cont.BeginParse()
		if err != nil {
			panic(err)
		}

		b = cell.BeginCell().
			MustStoreUInt(0x47, 7). // 0x8E >> 1 = 0x47
			MustStoreUInt(uint64(cont.RefsNum()), 2).
			MustStoreUInt(sz, 7).
			MustStoreSlice(codeSlice.MustLoadSlice(cont.BitsSize()), uint(sz*8))

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
	if cont := op.contCell(); cont != nil {
		str = cont.Dump()
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
		var err error
		code, err = beginPushRefCell(state, op.cont, op.contTrace)
		if err != nil {
			return err
		}
	} else if op.cont == nil && op.window.BaseCell() != nil {
		// decoded SMALL/BIG: the body is a window over the original code cell;
		// sharing is safe, continuation code is never advanced in place
		code = &op.window
	} else {
		var err error
		code, err = op.cont.BeginParse()
		if err != nil {
			return err
		}
	}

	return state.Stack.PushOwnedContinuation(&vm.OrdinaryContinuation{Code: code, Data: vm.ControlData{
		NumArgs: vm.ControlDataAllArgs,
		CP:      state.CP,
	}})
}
