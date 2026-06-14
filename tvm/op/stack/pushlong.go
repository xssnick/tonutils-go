package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpPUSHL struct {
	helpers.Prefixed
	stackIndex uint8
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHL(0) })
}

var pushLongPrefixed = helpers.SinglePrefixed(helpers.UIntPrefix(0x56, 8))

func PUSHL(index uint8) *OpPUSHL {
	return &OpPUSHL{
		Prefixed:   pushLongPrefixed,
		stackIndex: index,
	}
}

func (op *OpPUSHL) Deserialize(code *cell.Slice) error {
	prefix, err := code.LoadUInt(8)
	if err != nil {
		return err
	}
	if prefix != 0x56 {
		return vm.ErrCorruptedOpcode
	}

	val, err := code.LoadUInt(8)
	if err != nil {
		return err
	}
	op.stackIndex = uint8(val)
	return nil
}

func (op *OpPUSHL) Serialize() *cell.Builder {
	return cell.BeginCell().MustStoreUInt(0x56, 8).MustStoreUInt(uint64(op.stackIndex), 8)
}

func (op *OpPUSHL) SerializeText() string {
	return fmt.Sprintf("s%d PUSH", op.stackIndex)
}

func (op *OpPUSHL) InstructionBits() int64 {
	return 16
}

func (op *OpPUSHL) Interpret(state *vm.State) error {
	return state.Stack.PushAt(int(op.stackIndex))
}
