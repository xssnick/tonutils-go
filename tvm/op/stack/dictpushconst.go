package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
)

type OpDICTPUSHCONST struct {
	helpers.Prefixed
	cont *cell.Cell
	pfx  uint64
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return DICTPUSHCONST(nil) })
}

func DICTPUSHCONST(cont *cell.Cell) *OpDICTPUSHCONST {
	return &OpDICTPUSHCONST{
		Prefixed: helpers.SinglePrefixed(helpers.SlicePrefix(13, []byte{0xF4, 0xA4})),
		cont:     cont,
	}
}

func (op *OpDICTPUSHCONST) Deserialize(code *cell.Slice) error {
	_, err := code.LoadSlice(13)
	if err != nil {
		return err
	}

	ref, err := code.LoadMaybeRef()
	if err != nil {
		return err
	}

	pfx, err := code.LoadUInt(10)
	if err != nil {
		return err
	}

	op.pfx = pfx
	op.cont = ref.WithoutObserver().MustToCell()

	return nil
}

func (op *OpDICTPUSHCONST) Serialize() *cell.Builder {
	return cell.BeginCell().MustStoreSlice([]byte{0xF4, 0xA4}, 13).MustStoreMaybeRef(op.cont).MustStoreUInt(op.pfx, 10)
}

func (op *OpDICTPUSHCONST) SerializeText() string {
	str := "???"
	if op.cont != nil {
		str = op.cont.Dump()
	}
	return fmt.Sprintf("<%s> %d DICTPUSHCONST", str, op.pfx)
}

func (op *OpDICTPUSHCONST) InstructionBits() int64 {
	return 24
}

func (op *OpDICTPUSHCONST) Interpret(state *vm.State) error {
	if err := state.Stack.PushCell(op.cont); err != nil {
		return err
	}
	return state.Stack.PushInt(new(big.Int).SetUint64(op.pfx))
}
