package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpPOP struct {
	stackIndex uint8
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return POP(0) })
}

func POP(stackIndex uint8) *OpPOP {
	return &OpPOP{
		stackIndex: stackIndex,
	}
}

func (op *OpPOP) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{cell.BeginCell().MustStoreUInt(0x3, 4).EndCell().BeginParse()}
}

func (op *OpPOP) Deserialize(code *cell.Slice) error {
	prefix, err := code.LoadUInt(4)
	if err != nil {
		return err
	}

	if prefix == 0x3 {
		index, err := code.LoadUInt(4)
		if err != nil {
			return err
		}
		op.stackIndex = uint8(index)
		return nil
	}

	return vm.ErrCorruptedOpcode
}

func (op *OpPOP) Serialize() *cell.Builder {
	return helpers.Builder([]byte{0x30 | op.stackIndex})
}

func (op *OpPOP) SerializeText() string {
	return fmt.Sprintf("s%d POP", op.stackIndex)
}

func (op *OpPOP) Interpret(state *vm.State) error {
	if err := state.Stack.PopSwapAt(int(op.stackIndex)); err != nil {
		return err
	}
	return nil
}
