package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// Executing an operand-carrying opcode must not allocate. That is the whole
// point of keeping the operand in a value: one shared instance per opcode
// serves every execution, so nothing is built per instruction. The property is
// quiet to lose — a closure that captures the operand, an operand that escapes
// into the interpreter — and losing it costs roughly eight allocations per
// instruction, so it is asserted rather than assumed.
//
// The programs below are chosen to do no allocating work of their own. That
// rules out both ends of the stack's integer handling: a result the stack has
// to canonicalize, and an interned constant, which PopInt defensively copies so
// an opcode cannot mutate it. Adding zero to a plain value touches neither, so
// anything the counter sees is the instruction machinery itself.
func TestArgOpcodeStepDoesNotAllocate(t *testing.T) {
	tests := []struct {
		name string
		code *cell.Cell
		// seed puts an integer on the stack so the program can be pure
		// arithmetic, without a PUSHINT whose own cost would be counted.
		seed bool
	}{
		{
			// NOP four times: an opcode with no operand at all.
			name: "no_operand",
			code: cell.BeginCell().
				MustStoreUInt(0x00, 8).
				MustStoreUInt(0x00, 8).
				MustStoreUInt(0x00, 8).
				MustStoreUInt(0x00, 8).
				EndCell(),
		},
		{
			// ADDINT 0 three times over a stack seeded with a plain value: an
			// operand is read and used on every step, and the value travels
			// through the stack untouched.
			name: "with_operand",
			code: cell.BeginCell().
				MustStoreUInt(0xA6, 8).MustStoreInt(0, 8).
				MustStoreUInt(0xA6, 8).MustStoreInt(0, 8).
				MustStoreUInt(0xA6, 8).MustStoreInt(0, 8).
				EndCell(),
			seed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			machine := NewTVM()
			dispatch := machine.dispatchForVersion(vm.MaxSupportedGlobalVersion)

			step := func() {
				state := newArgAllocTestState(t, tt.code, tt.seed)
				for state.CurrentCode.BitsLeft() > 0 {
					if err := machine.stepWithDispatch(dispatch, state); err != nil {
						t.Fatalf("step: %v", err)
					}
				}
			}
			step()

			setup := testing.AllocsPerRun(100, func() {
				newArgAllocTestState(t, tt.code, tt.seed)
			})
			total := testing.AllocsPerRun(100, step)

			if stepping := total - setup; stepping > 0 {
				t.Fatalf("stepping allocated %.1f objects, want 0 (setup %.1f, total %.1f)", stepping, setup, total)
			}
		})
	}
}

func newArgAllocTestState(t *testing.T, code *cell.Cell, seed bool) *vm.State {
	t.Helper()

	stack := vm.NewStack()
	if seed {
		if err := stack.PushInt(big.NewInt(100)); err != nil {
			t.Fatalf("seed stack: %v", err)
		}
	}

	state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.GasWithLimit(1_000_000), nil, tuple.Tuple{}, stack)
	if err := state.JumpToCode(code.MustBeginParse(), 0); err != nil {
		t.Fatalf("jump to code: %v", err)
	}
	return state
}
