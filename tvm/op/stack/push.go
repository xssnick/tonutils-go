package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpPUSH struct {
	stackIndex uint8
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSH(0) })
}

func PUSH(stackIndex uint8) *OpPUSH {
	return &OpPUSH{
		stackIndex: stackIndex,
	}
}

func (op *OpPUSH) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{cell.BeginCell().MustStoreUInt(0x2, 4).EndCell().BeginParse()}
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
