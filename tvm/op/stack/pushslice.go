package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type OpPUSHREFSLICE struct {
	helpers.Prefixed
	value *cell.Slice
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHSLICE(cell.BeginCell().ToSlice()) })
}

func PUSHSLICE(value *cell.Slice) *OpPUSHREFSLICE {
	return &OpPUSHREFSLICE{
		Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x89, 8)),
		value:    value.Copy(),
	}
}

func (op *OpPUSHREFSLICE) Deserialize(code *cell.Slice) error {
	_, err := code.LoadUInt(8)
	if err != nil {
		return err
	}

	refCell, err := code.PeekRefCell()
	if err != nil {
		return vmerr.Error(vmerr.CodeInvalidOpcode, "no references left for a PUSHREF instruction")
	}
	if err = code.AdvanceExt(0, 1); err != nil {
		return err
	}

	op.value = refCell.BeginParse()
	return nil
}

func (op *OpPUSHREFSLICE) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreUInt(0x89, 8).MustStoreRef(op.value.MustToCell())
}

func (op *OpPUSHREFSLICE) SerializeText() string {
	str := "???"
	if op.value != nil {
		str = op.value.WithoutObserver().MustToCell().Dump()
	}
	return fmt.Sprintf("%s PUSHREFSLICE", str)
}

func (op *OpPUSHREFSLICE) InstructionBits() int64 {
	return 8
}

func (op *OpPUSHREFSLICE) Interpret(state *vm.State) error {
	return state.Stack.PushSlice(op.value)
}
