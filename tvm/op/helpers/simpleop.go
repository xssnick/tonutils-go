package helpers

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type SimpleOP struct {
	Action       func(*vm.State) error
	BitPrefix    BitPrefix
	Name         string
	BaseGasPrice int64
}

func (op *SimpleOP) prefix() BitPrefix {
	return op.BitPrefix
}

func (op *SimpleOP) GetPrefixes() []*cell.Slice {
	return PrefixSlices(op.prefix())
}

func (op *SimpleOP) Deserialize(code *cell.Slice) error {
	return op.DeserializeMatched(code)
}

func (op *SimpleOP) DeserializeMatched(code *cell.Slice) error {
	_, err := code.LoadSlice(op.prefix().Bits)
	return err
}

func (op *SimpleOP) Serialize() *cell.Builder {
	prefix := op.prefix()
	return cell.BeginCell().MustStoreSlice(prefix.Data, prefix.Bits)
}

func (op *SimpleOP) SerializeText() string {
	return op.Name
}

func (op *SimpleOP) InstructionBits() int64 {
	return int64(op.prefix().Bits)
}

func (op *SimpleOP) Interpret(state *vm.State) error {
	if op.BaseGasPrice != 0 {
		if err := state.ConsumeGas(op.BaseGasPrice); err != nil {
			return err
		}
	}
	return op.Action(state)
}
