package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpPUSHCONT struct {
	cont *cell.Cell
	typ  string
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHCONT(nil) })
}

func PUSHCONT(cont *cell.Cell) *OpPUSHCONT {
	return &OpPUSHCONT{
		cont: cont,
	}
}

func (op *OpPUSHCONT) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{
		cell.BeginCell().MustStoreUInt(0x9, 4).EndCell().BeginParse(),
		cell.BeginCell().MustStoreUInt(0x47, 7).EndCell().BeginParse(),
		cell.BeginCell().MustStoreUInt(0x8A, 8).EndCell().BeginParse(),
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
			return err
		}

		op.typ = "BIG"

		prefix = (prefix << 3) | prefix2
		if prefix != 0x47 {
			prefix3, err := code.LoadUInt(1)
			if err != nil {
				return err
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
			return err
		}

		data, err := code.LoadSlice(uint(szBytes * 8))
		if err != nil {
			return err
		}

		op.cont = cell.BeginCell().MustStoreSlice(data, uint(szBytes*8)).EndCell()
		return nil
	case "BIG":
		refsNum, err := code.LoadUInt(2)
		if err != nil {
			return err
		}

		szBytes, err := code.LoadUInt(7)
		if err != nil {
			return err
		}

		data, err := code.LoadSlice(uint(szBytes * 8))
		if err != nil {
			return err
		}

		cn := cell.BeginCell().MustStoreSlice(data, uint(szBytes*8))

		for i := 0; i < int(refsNum); i++ {
			ref, err := code.LoadRefCell()
			if err != nil {
				return err
			}

			err = cn.StoreRef(ref)
			if err != nil {
				return err
			}
		}

		op.cont = cn.EndCell()
		return nil
	case "REF":
		ref, err := code.LoadRefCell()
		if err != nil {
			return err
		}

		op.cont = ref
		return nil
	}

	return vm.ErrCorruptedOpcode
}

func (op *OpPUSHCONT) Serialize() *cell.Builder {
	var b *cell.Builder
	switch {
	case op.cont.RefsNum() == 0 && op.cont.BitsSize() <= 16*8:
		op.typ = "SMALL"

		sz := uint64(op.cont.BitsSize() / 8)
		if op.cont.BitsSize()%8 != 0 {
			sz += 1
		}

		b = cell.BeginCell().
			MustStoreUInt(9, 4).
			MustStoreUInt(sz, 4).
			MustStoreSlice(op.cont.BeginParse().MustLoadSlice(op.cont.BitsSize()), uint(sz*8))
	case op.cont.RefsNum() <= 3:
		op.typ = "BIG"

		sz := uint64(op.cont.BitsSize() / 8)
		if op.cont.BitsSize()%8 != 0 {
			sz += 1
		}

		codeSlice := op.cont.BeginParse()

		b = cell.BeginCell().
			MustStoreUInt(0x47, 7). // 0x8E >> 1 = 0x47
			MustStoreUInt(uint64(op.cont.RefsNum()), 2).
			MustStoreUInt(sz, 7).
			MustStoreSlice(codeSlice.MustLoadSlice(op.cont.BitsSize()), uint(sz*8))

		for codeSlice.RefsNum() > 0 {
			b.MustStoreRef(codeSlice.MustLoadRef().MustToCell())
		}
	default:
		op.typ = "REF"

		b = cell.BeginCell().
			MustStoreUInt(0x8A, 8).MustStoreRef(op.cont)
	}

	return b
}

func (op *OpPUSHCONT) SerializeText() string {
	str := "???"
	if op.cont != nil {
		str = op.cont.Dump()
	}
	return fmt.Sprintf("<%s> %s PUSHCONT", str, op.typ)
}

func (op *OpPUSHCONT) Interpret(state *vm.State) error {
	return state.Stack.PushContinuation(&vm.OrdinaryContinuation{Code: op.cont.BeginParse(), Data: vm.ControlData{
		NumArgs: vm.ControlDataAllArgs,
		CP:      state.CP,
	}})
}
