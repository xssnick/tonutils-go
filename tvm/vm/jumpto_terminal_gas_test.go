package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// The reference jump_to counts and charges the terminating iteration of a
// nested continuation chain too: when the chain ends in a quit continuation,
// cnt is still incremented and consume_gas(1) still runs (and running out of
// gas there overrides the exit).
func TestJumpToChargesTerminalQuitIteration(t *testing.T) {
	buildChain := func(layers int) Continuation {
		var c Continuation = &QuitContinuation{ExitCode: 0}
		for i := 0; i < layers; i++ {
			c = &PushIntContinuation{Int: int64(i), Next: c}
		}
		return c
	}

	run := func(version, layers int) (int64, error) {
		state := NewExecutionState(version, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
		err := state.JumpTo(buildChain(layers))
		return state.Gas.Used(), err
	}

	// 10 wrappers + quit = 11 iterations; iterations 9..11 are paid at v9+.
	gasUsed, err := run(9, 10)
	if !IsHandledException(err) {
		t.Fatalf("expected quit signal, got %v", err)
	}
	if code, _ := vmerr.ErrorCode(err); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if gasUsed != 3 {
		t.Fatalf("v9 nested-jump gas = %d, want 3 (terminal quit iteration must be charged)", gasUsed)
	}

	// Below v9 nested jumps are free.
	gasUsed, err = run(8, 10)
	if !IsHandledException(err) {
		t.Fatalf("expected quit signal, got %v", err)
	}
	if gasUsed != 0 {
		t.Fatalf("v8 nested-jump gas = %d, want 0", gasUsed)
	}

	// Exactly free_nested_cont_jump iterations stay free: 7 wrappers + quit.
	gasUsed, err = run(9, 7)
	if !IsHandledException(err) {
		t.Fatalf("expected quit signal, got %v", err)
	}
	if gasUsed != 0 {
		t.Fatalf("v9 short-chain gas = %d, want 0", gasUsed)
	}

	// Out of gas on the terminal charge overrides the quit exit code.
	state := NewExecutionState(9, GasWithLimit(2), nil, tuple.Tuple{}, NewStack())
	err = state.JumpTo(buildChain(10))
	if code, _ := vmerr.ErrorCode(err); code != vmerr.CodeOutOfGas || IsHandledException(err) {
		t.Fatalf("expected unhandled out-of-gas to override quit, got %v", err)
	}
}
