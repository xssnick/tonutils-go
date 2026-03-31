package helpers

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type AdvancedOP struct {
	Action            func(*vm.State) error
	BitPrefix         BitPrefix
	NameSerializer    func() string
	SerializeSuffix   func() *cell.Builder
	DeserializeSuffix func(code *cell.Slice) error
	BaseGasPrice      int64
	FixedSizeBits     int64
}

func (op *AdvancedOP) prefix() BitPrefix {
	return op.BitPrefix
}

func (op *AdvancedOP) GetPrefixes() []*cell.Slice {
	return PrefixSlices(op.prefix())
}

func (op *AdvancedOP) Deserialize(code *cell.Slice) error {
	return op.DeserializeMatched(code)
}

func (op *AdvancedOP) DeserializeMatched(code *cell.Slice) error {
	if _, err := code.LoadSlice(op.prefix().Bits); err != nil {
		return err
	}
	if op.DeserializeSuffix != nil {
		return op.DeserializeSuffix(code)
	}
	return nil
}

func (op *AdvancedOP) Serialize() *cell.Builder {
	prefix := op.prefix()
	b := cell.BeginCell().MustStoreSlice(prefix.Data, prefix.Bits)
	if op.SerializeSuffix != nil {
		b.MustStoreBuilder(op.SerializeSuffix())
	}
	return b
}

func (op *AdvancedOP) SerializeText() string {
	return op.NameSerializer()
}

func (op *AdvancedOP) InstructionBits() int64 {
	return int64(op.prefix().Bits) + op.FixedSizeBits
}

func (op *AdvancedOP) Interpret(state *vm.State) error {
	if op.BaseGasPrice != 0 {
		if err := state.ConsumeGas(op.BaseGasPrice); err != nil {
			return err
		}
	}
	return op.Action(state)
}
