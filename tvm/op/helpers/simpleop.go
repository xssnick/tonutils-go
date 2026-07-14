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
	MinVersion   int
}

func (op *SimpleOP) GetPrefixes() []*cell.Slice {
	return PrefixSlices(op.BitPrefix)
}

func (op *SimpleOP) Deserialize(code *cell.Slice) error {
	return op.DeserializeMatched(code)
}

func (op *SimpleOP) DeserializeMatched(code *cell.Slice) error {
	return code.SkipBits(op.BitPrefix.Bits)
}

func (op *SimpleOP) Serialize() *cell.Builder {
	return cell.BeginCell().MustStoreSlice(op.BitPrefix.Data, op.BitPrefix.Bits)
}

func (op *SimpleOP) SerializeText() string {
	return op.Name
}

func (op *SimpleOP) InstructionBits() int64 {
	return int64(op.BitPrefix.Bits)
}

func (op *SimpleOP) MinGlobalVersion() int {
	return op.MinVersion
}

func (op *SimpleOP) Reusable() bool {
	return true
}

func (op *SimpleOP) Interpret(state *vm.State) error {
	if op.BaseGasPrice != 0 {
		if err := state.ConsumeGas(op.BaseGasPrice); err != nil {
			return err
		}
	}
	return op.Action(state)
}
