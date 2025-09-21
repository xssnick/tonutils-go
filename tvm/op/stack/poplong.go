package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpPOPL struct {
	stackIndex uint8
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return POPL(0) })
}

func POPL(index uint8) *OpPOPL {
	return &OpPOPL{stackIndex: index}
}

func (op *OpPOPL) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{cell.BeginCell().MustStoreUInt(0x57, 8).EndCell().BeginParse()}
}

func (op *OpPOPL) Deserialize(code *cell.Slice) error {
	val, err := code.LoadUInt(8)
	if err != nil {
		return err
	}
	op.stackIndex = uint8(val)
	return nil
}

func (op *OpPOPL) Serialize() *cell.Builder {
	return cell.BeginCell().MustStoreUInt(0x57, 8).MustStoreUInt(uint64(op.stackIndex), 8)
}

func (op *OpPOPL) SerializeText() string {
	return fmt.Sprintf("s%d POP", op.stackIndex)
}

func (op *OpPOPL) Interpret(state *vm.State) error {
	return state.Stack.PopSwapAt(int(op.stackIndex))
}
