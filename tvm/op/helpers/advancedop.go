package helpers

import (
	"bytes"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type AdvancedOP struct {
	Action            func(*vm.State) error
	Prefix            *cell.Cell
	NameSerializer    func() string
	SerializeSuffix   func() *cell.Builder
	DeserializeSuffix func(code *cell.Slice) error
	BaseGasPrice      uint64
}

func (op *AdvancedOP) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{
		op.Prefix.BeginParse(),
	}
}

func (op *AdvancedOP) Deserialize(code *cell.Slice) error {
	prefix, err := code.LoadSlice(op.Prefix.BitsSize())
	if err != nil {
		return err
	}

	pCell := cell.BeginCell().MustStoreSlice(prefix, op.Prefix.BitsSize()).EndCell()
	if !bytes.Equal(pCell.Hash(), op.Prefix.Hash()) {
		return vm.ErrCorruptedOpcode
	}

	if op.DeserializeSuffix != nil {
		return op.DeserializeSuffix(code)
	}

	return nil
}

func (op *AdvancedOP) Serialize() *cell.Builder {
	b := op.Prefix.ToBuilder()
	if op.SerializeSuffix != nil {
		b.MustStoreBuilder(op.SerializeSuffix())
	}
	return b
}

func (op *AdvancedOP) SerializeText() string {
	return op.NameSerializer()
}

func (op *AdvancedOP) Interpret(state *vm.State) error {
	if err := state.Gas.Consume(op.BaseGasPrice); err != nil {
		return err
	}
	return op.Action(state)
}
