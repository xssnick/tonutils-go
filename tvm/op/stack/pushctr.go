package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpPUSHCTR struct {
	ctrIndex uint8
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHCTR(0) })
}

func PUSHCTR(ctrIndex uint8) *OpPUSHCTR {
	return &OpPUSHCTR{
		ctrIndex: ctrIndex,
	}
}

func (op *OpPUSHCTR) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{cell.BeginCell().MustStoreUInt(0xED4, 12).EndCell().BeginParse()}
}

func (op *OpPUSHCTR) Deserialize(code *cell.Slice) error {
	data, err := code.LoadUInt(16)
	if err != nil {
		return err
	}
	op.ctrIndex = uint8(data & 0xF)
	return nil
}

func (op *OpPUSHCTR) Serialize() *cell.Builder {
	return helpers.Builder([]byte{0xED, 0x40 | (op.ctrIndex & 0xF)})
}

func (op *OpPUSHCTR) SerializeText() string {
	return fmt.Sprintf("c%d PUSH", op.ctrIndex)
}

func (op *OpPUSHCTR) Interpret(state *vm.State) error {
	return state.Stack.PushAny(state.Reg.Get(int(op.ctrIndex)))
}
