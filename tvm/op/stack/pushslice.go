package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpPUSHSLICE struct {
	value *cell.Slice
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHSLICE(cell.BeginCell().ToSlice()) })
}

func PUSHSLICE(value *cell.Slice) *OpPUSHSLICE {
	return &OpPUSHSLICE{
		value: value.Copy(),
	}
}

func (op *OpPUSHSLICE) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{
		cell.BeginCell().MustStoreUInt(0x89, 8).EndCell().BeginParse(),
	}
}

func (op *OpPUSHSLICE) Deserialize(code *cell.Slice) error {
	prefix, err := code.LoadUInt(8)
	if err != nil {
		return err
	}

	switch prefix {
	case 0x89:
		ref, err := code.LoadRef()
		if err != nil {
			return err
		}

		op.value = ref
		return nil
	}

	return vm.ErrCorruptedOpcode
}

func (op *OpPUSHSLICE) Serialize() *cell.Builder {
	var b *cell.Builder
	switch {
	// TODO: other types
	default:
		b = cell.BeginCell().
			MustStoreUInt(0x89, 8).MustStoreRef(op.value.MustToCell())
	}

	return b
}

func (op *OpPUSHSLICE) SerializeText() string {
	str := "???"
	if op.value != nil {
		str = op.value.MustToCell().Dump()
	}
	return fmt.Sprintf("%s PUSHSLICE", str)
}

func (op *OpPUSHSLICE) Interpret(state *vm.State) error {
	return state.Stack.Push(op.value)
}
