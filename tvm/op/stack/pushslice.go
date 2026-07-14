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
	ref *cell.Cell
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHSLICE(cell.BeginCell().ToSlice()) })
}

func PUSHSLICE(value *cell.Slice) *OpPUSHREFSLICE {
	return &OpPUSHREFSLICE{
		Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x89, 8)),
		ref:      value.Copy().MustToCell(),
	}
}

func (op *OpPUSHREFSLICE) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(8); err != nil {
		return err
	}

	refCell, err := code.PeekRefCell()
	if err != nil {
		return vmerr.Error(vmerr.CodeInvalidOpcode, "no references left for a PUSHREF instruction")
	}
	if err = code.SkipBitsAndRefs(0, 1); err != nil {
		return err
	}

	op.ref = refCell
	return nil
}

func (op *OpPUSHREFSLICE) Serialize() *cell.Builder {
	return cell.BeginCell().MustStoreUInt(0x89, 8).MustStoreRef(op.ref)
}

func (op *OpPUSHREFSLICE) SerializeText() string {
	str := "???"
	if op.ref != nil {
		str = op.ref.WithoutTrace().Dump()
	}
	return fmt.Sprintf("%s PUSHREFSLICE", str)
}

func (op *OpPUSHREFSLICE) InstructionBits() int64 {
	return 8
}

func (op *OpPUSHREFSLICE) Interpret(state *vm.State) error {
	value, err := beginPushRefCell(state, op.ref)
	if err != nil {
		return err
	}
	return state.Stack.PushOwnedSlice(value)
}
