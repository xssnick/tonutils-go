package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type OpPUSHREF struct {
	helpers.Prefixed
	value *cell.Cell
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHREF(nil) })
}

func PUSHREF(value *cell.Cell) *OpPUSHREF {
	return &OpPUSHREF{
		Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x88, 8)),
		value:    value,
	}
}

func (op *OpPUSHREF) Deserialize(code *cell.Slice) error {
	if _, err := code.LoadUInt(8); err != nil {
		return err
	}

	refCell, err := code.PeekRefCell()
	if err != nil {
		return vmerr.Error(vmerr.CodeInvalidOpcode, "no references left for a PUSHREF instruction")
	}
	if err = code.AdvanceExt(0, 1); err != nil {
		return err
	}

	op.value = refCell
	return nil
}

func (op *OpPUSHREF) Serialize() *cell.Builder {
	return cell.BeginCell().MustStoreUInt(0x88, 8).MustStoreRef(op.value)
}

func (op *OpPUSHREF) SerializeText() string {
	str := "???"
	if op.value != nil {
		str = op.value.Dump()
	}
	return fmt.Sprintf("%s PUSHREF", str)
}

func (op *OpPUSHREF) InstructionBits() int64 {
	return 8
}

func (op *OpPUSHREF) Interpret(state *vm.State) error {
	return state.Stack.PushCell(op.value)
}

func PUSHREFSLICE(value *cell.Slice) *OpPUSHREFSLICE {
	return PUSHSLICE(value)
}
