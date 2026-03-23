package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpPUSH struct {
	helpers.Prefixed
	stackIndex uint8
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSH(0) })
}

func PUSH(stackIndex uint8) *OpPUSH {
	return &OpPUSH{
		Prefixed:   helpers.SinglePrefixed(helpers.UIntPrefix(0x2, 4)),
		stackIndex: stackIndex,
	}
}

func (op *OpPUSH) Deserialize(code *cell.Slice) error {
	prefix, err := code.LoadUInt(4)
	if err != nil {
		return err
	}

	if prefix == 0x02 {
		index, err := code.LoadUInt(4)
		if err != nil {
			return err
		}
		op.stackIndex = uint8(index)
		return nil
	}

	return vm.ErrCorruptedOpcode
}

func (op *OpPUSH) Serialize() *cell.Builder {
	return helpers.Builder([]byte{0x20 | op.stackIndex})
}

func (op *OpPUSH) SerializeText() string {
	return fmt.Sprintf("s%d PUSH", op.stackIndex)
}

func (op *OpPUSH) Interpret(state *vm.State) error {
	return state.Stack.PushAt(int(op.stackIndex))
}
