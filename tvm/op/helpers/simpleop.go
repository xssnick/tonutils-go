package helpers

import (
	"bytes"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type SimpleOP struct {
	Action       func(*vm.State) error
	Prefix       []byte
	Name         string
	BaseGasPrice uint64
}

func (op *SimpleOP) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{
		cell.BeginCell().MustStoreSlice(op.Prefix, uint(8*len(op.Prefix))).EndCell().BeginParse(),
	}
}

func (op *SimpleOP) Deserialize(code *cell.Slice) error {
	prefix, err := code.LoadSlice(8 * uint(len(op.Prefix)))
	if err != nil {
		return err
	}

	if !bytes.HasPrefix(prefix, op.Prefix) {
		return vm.ErrCorruptedOpcode
	}

	return nil
}

func (op *SimpleOP) Serialize() *cell.Builder {
	return Builder(op.Prefix)
}

func (op *SimpleOP) SerializeText() string {
	return op.Name
}

func (op *SimpleOP) Interpret(state *vm.State) error {
	if err := state.Gas.Consume(op.BaseGasPrice); err != nil {
		return err
	}
	return op.Action(state)
}
