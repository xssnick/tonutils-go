package vm

import "github.com/xssnick/tonutils-go/tvm/cell"

// ArgOP is an opcode whose decoded operand travels as a value rather than
// living inside the instruction object.
//
// The older shape bound the two together: the operand was written back into
// fields captured by the opcode's own closures, so every executed instruction
// needed a freshly built object — and with it a big.Int for the operand and one
// allocation per closure. Keeping the operand in a value instead lets a single
// immutable instance be registered per opcode and shared by every execution, so
// a step allocates nothing at all. It is also what makes the shared instance
// safe: nothing is ever written back, so two goroutines executing the same
// opcode cannot observe each other.
//
// Opcodes whose operand is bulk data rather than a number decode into the
// execution state and return whatever scalar the gas computation and the
// interpreter still need — the state belongs to one execution, so it carries
// per-instruction data without being shared.
type ArgOP interface {
	GetPrefixes() []*cell.Slice

	// DecodeArgs consumes the whole instruction — prefix included — from code
	// and returns its operand. It must not retain code.
	//
	// A truncated instruction still gets charged for, so on failure it must
	// return whatever it had already determined that ArgInstructionBits needs
	// to pick the right length.
	DecodeArgs(state *State, code *cell.Slice) (uint64, error)

	// ArgInstructionBits is the instruction length gas is charged for. It is
	// not always what DecodeArgs consumed: an opcode carrying an inline body
	// pays for its header only.
	ArgInstructionBits(args uint64) int64

	InterpretArgs(state *State, args uint64) error

	SerializeArgs(args uint64) *cell.Builder
	SerializeArgsText(args uint64) string
}

// ArgList is the registry of operand-carrying opcodes. It is kept apart from
// List because these opcodes deliberately have no Deserialize/Interpret pair of
// their own — that pair is exactly what used to force an instance per
// instruction.
var ArgList []ArgOP

// AllOps is every registered opcode, whichever registry it lives in, presented
// through the plain OP interface. Operand-carrying opcodes come back bound to a
// zero operand, which is what inspecting a registry wants: prefixes, versions
// and names do not depend on the operand. Every call of such a getter mints an
// independent binding, since Deserialize writes the decoded operand into it.
//
// Anything enumerating the instruction set — coverage audits, differential
// fuzzing — must go through this rather than List, or it silently stops seeing
// every opcode that keeps its operand in a value.
func AllOps() []OPGetter {
	ops := make([]OPGetter, 0, len(List)+len(ArgList))
	ops = append(ops, List...)
	for _, op := range ArgList {
		ops = append(ops, func() OP { return Bind(op, 0) })
	}
	return ops
}

// Bind pairs an opcode with one operand so it can be driven through the plain
// OP interface. The dispatcher never needs this, since it keeps the operand in
// a value; assembling a specific instruction and testing one opcode in
// isolation both do, because they want to name an instruction together with its
// argument.
func Bind(op ArgOP, args uint64) OP {
	return &boundArgOP{op: op, args: args}
}

type boundArgOP struct {
	op   ArgOP
	args uint64
}

func (b *boundArgOP) GetPrefixes() []*cell.Slice {
	return b.op.GetPrefixes()
}

func (b *boundArgOP) Deserialize(code *cell.Slice) error {
	args, err := b.op.DecodeArgs(nil, code)
	b.args = args
	return err
}

func (b *boundArgOP) Interpret(state *State) error {
	return b.op.InterpretArgs(state, b.args)
}

func (b *boundArgOP) Serialize() *cell.Builder {
	return b.op.SerializeArgs(b.args)
}

func (b *boundArgOP) SerializeText() string {
	return b.op.SerializeArgsText(b.args)
}

func (b *boundArgOP) InstructionBits() int64 {
	return b.op.ArgInstructionBits(b.args)
}

func (b *boundArgOP) MinGlobalVersion() int {
	if versioned, ok := b.op.(VersionedOp); ok {
		return versioned.MinGlobalVersion()
	}
	return 0
}

// Unwrap exposes the shared opcode behind a binding.
func (b *boundArgOP) Unwrap() ArgOP {
	return b.op
}
