package cellslice

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LDU(0) })
}

// OpLDU is a struct-based opcode: one allocation per executed instruction
// instead of an AdvancedOP carrying per-instance closures.
type OpLDU struct {
	sz uint
}

var lduPrefix = helpers.BytesPrefix(0xD3)

func LDU(sz uint) *OpLDU {
	return &OpLDU{sz: sz}
}

func (op *OpLDU) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(lduPrefix)
}

func (op *OpLDU) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(lduPrefix.Bits); err != nil {
		return err
	}
	val, err := code.LoadUInt(8)
	if err != nil {
		return err
	}
	op.sz = uint(val) + 1
	return nil
}

func (op *OpLDU) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreSlice(lduPrefix.Data, lduPrefix.Bits).
		MustStoreUInt(uint64(op.sz-1), 8)
}

func (op *OpLDU) SerializeText() string {
	return fmt.Sprintf("%d LDU", op.sz)
}

func (op *OpLDU) InstructionBits() int64 {
	return int64(lduPrefix.Bits) + 8
}

func (op *OpLDU) Interpret(state *vm.State) error {
	s0, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}

	if op.sz <= 64 {
		v, err := s0.LoadUInt(op.sz)
		if err != nil {
			return err
		}
		if v <= 1<<63-1 {
			err = state.Stack.PushSmallInt(int64(v))
		} else {
			err = state.Stack.PushOwnedInt(new(big.Int).SetUint64(v))
		}
		if err != nil {
			return err
		}

		return state.Stack.PushOwnedSlice(s0)
	}

	i, err := s0.LoadBigUInt(op.sz)
	if err != nil {
		return err
	}

	err = state.Stack.PushOwnedInt(i)
	if err != nil {
		return err
	}

	return state.Stack.PushOwnedSlice(s0)
}
