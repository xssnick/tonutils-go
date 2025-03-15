package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type OpPUSHREFSLICE struct {
	value *cell.Slice
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHSLICE(cell.BeginCell().ToSlice()) })
}

func PUSHSLICE(value *cell.Slice) *OpPUSHREFSLICE {
	return &OpPUSHREFSLICE{
		value: value.Copy(),
	}
}

func (op *OpPUSHREFSLICE) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{
		cell.BeginCell().MustStoreUInt(0x89, 8).EndCell().BeginParse(),
	}
}

func (op *OpPUSHREFSLICE) Deserialize(code *cell.Slice) error {
	_, err := code.LoadUInt(8)
	if err != nil {
		return err
	}

	ref, err := code.LoadRef()
	if err != nil {
		return vmerr.VMError{
			Code: vmerr.ErrInvalidOpcode.Code,
			Msg:  "no references left for a PUSHREF instruction",
		}
	}

	op.value = ref
	return nil
}

func (op *OpPUSHREFSLICE) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreUInt(0x89, 8).MustStoreRef(op.value.MustToCell())
}

func (op *OpPUSHREFSLICE) SerializeText() string {
	str := "???"
	if op.value != nil {
		str = op.value.MustToCell().Dump()
	}
	return fmt.Sprintf("%s PUSHREFSLICE", str)
}

func (op *OpPUSHREFSLICE) Interpret(state *vm.State) error {
	return state.Stack.PushSlice(op.value)
}
