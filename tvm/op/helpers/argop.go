package helpers

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// ArgOP is the shared implementation behind every opcode that carries an
// operand. One instance is built per opcode at registration and is then
// immutable: the operand a step decodes is returned as a value and passed on to
// Action, so executing an instruction allocates nothing.
//
// The common case needs no code at all beyond Action and Name — a fixed-width
// operand right after the prefix is read by the default DecodeArgs. Opcodes with
// a shape of their own override Decode, and those whose charged length differs
// from what they consume override Bits.
type ArgOP struct {
	Prefixed

	// ArgBits is the width of the operand following the prefix. Ignored when
	// Decode is set.
	ArgBits uint

	Action func(state *vm.State, args uint64) error
	Name   func(args uint64) string

	// Decode replaces the default read. It is handed the instruction with the
	// prefix still in front of it and must consume the whole of it.
	Decode func(state *vm.State, code *cell.Slice) (uint64, error)
	// Bits replaces the default charged length of prefix + ArgBits.
	Bits func(args uint64) int64
	// Serializer replaces the default prefix-then-operand encoding.
	Serializer func(args uint64) *cell.Builder

	BaseGasPrice int64
	MinVersion   int
}

// NewArgOP builds an operand-carrying opcode and checks the invariant the
// default decoder relies on: with several prefixes they must all be the same
// length, or the decoder could not know how far to skip. Opcodes that really do
// mix lengths carry their own Decode.
func NewArgOP(op *ArgOP) *ArgOP {
	if op.Decode == nil {
		for _, p := range op.Prefixes {
			if p.Bits != op.Prefixes[0].Bits {
				panic("helpers: ArgOP with prefixes of differing length needs its own Decode")
			}
		}
	}
	return op
}

func (op *ArgOP) prefixBits() uint {
	return op.Prefixes[0].Bits
}

func (op *ArgOP) DecodeArgs(state *vm.State, code *cell.Slice) (uint64, error) {
	if op.Decode != nil {
		return op.Decode(state, code)
	}
	if err := code.SkipBits(op.prefixBits()); err != nil {
		return 0, err
	}
	if op.ArgBits == 0 {
		return 0, nil
	}
	return code.LoadUInt(op.ArgBits)
}

func (op *ArgOP) ArgInstructionBits(args uint64) int64 {
	if op.Bits != nil {
		return op.Bits(args)
	}
	return int64(op.prefixBits() + op.ArgBits)
}

func (op *ArgOP) InterpretArgs(state *vm.State, args uint64) error {
	if op.BaseGasPrice != 0 {
		if err := state.ConsumeGas(op.BaseGasPrice); err != nil {
			return err
		}
	}
	return op.Action(state, args)
}

func (op *ArgOP) SerializeArgs(args uint64) *cell.Builder {
	if op.Serializer != nil {
		return op.Serializer(args)
	}
	b := cell.BeginCell().MustStoreSlice(op.Prefixes[0].Data, op.Prefixes[0].Bits)
	if op.ArgBits > 0 {
		b.MustStoreUInt(args, op.ArgBits)
	}
	return b
}

func (op *ArgOP) SerializeArgsText(args uint64) string {
	return op.Name(args)
}

func (op *ArgOP) MinGlobalVersion() int {
	return op.MinVersion
}
