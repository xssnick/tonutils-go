package stack

import (
	"math/big"
	"testing"
)

func TestLowBranchStackOps(t *testing.T) {
	t.Run("RotFamilyAndSwap2", func(t *testing.T) {
		state := newStackState(1, 2, 3)
		if err := ROT().Interpret(state); err != nil {
			t.Fatalf("ROT failed: %v", err)
		}
		if got := popInts(t, state.Stack, 3); !equalInts(got, []int64{1, 3, 2}) {
			t.Fatalf("unexpected ROT stack: %v", got)
		}

		state = newStackState(1, 2, 3)
		if err := ROTREV().Interpret(state); err != nil {
			t.Fatalf("ROTREV failed: %v", err)
		}
		if got := popInts(t, state.Stack, 3); !equalInts(got, []int64{2, 1, 3}) {
			t.Fatalf("unexpected ROTREV stack: %v", got)
		}

		state = newStackState(1, 2, 3, 4)
		if err := SWAP2().Interpret(state); err != nil {
			t.Fatalf("SWAP2 failed: %v", err)
		}
		if got := popInts(t, state.Stack, 4); !equalInts(got, []int64{2, 1, 4, 3}) {
			t.Fatalf("unexpected SWAP2 stack: %v", got)
		}

		if err := ROT().Interpret(newStackState(1, 2)); err == nil {
			t.Fatal("ROT should fail on a stack shorter than 3 values")
		}
		if err := ROTREV().Interpret(newStackState(1, 2)); err == nil {
			t.Fatal("ROTREV should fail on a stack shorter than 3 values")
		}
		if err := SWAP2().Interpret(newStackState(1, 2, 3)); err == nil {
			t.Fatal("SWAP2 should fail on a stack shorter than 4 values")
		}
	})

	t.Run("RevxAndBlkswxShortCircuits", func(t *testing.T) {
		state := newStackState(1, 2, 3)
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("failed to push REVX x: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push REVX y: %v", err)
		}
		if err := REVX().Interpret(state); err != nil {
			t.Fatalf("REVX no-op failed: %v", err)
		}
		if got := popInts(t, state.Stack, 3); !equalInts(got, []int64{3, 2, 1}) {
			t.Fatalf("unexpected REVX no-op stack: %v", got)
		}

		state = newStackState(1, 2, 3)
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push BLKSWX x: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("failed to push BLKSWX y: %v", err)
		}
		if err := BLKSWX().Interpret(state); err != nil {
			t.Fatalf("BLKSWX no-op failed: %v", err)
		}
		if got := popInts(t, state.Stack, 3); !equalInts(got, []int64{3, 2, 1}) {
			t.Fatalf("unexpected BLKSWX no-op stack: %v", got)
		}

		state = newStackState(1, 2)
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push REVX bad x: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push REVX bad y: %v", err)
		}
		if err := REVX().Interpret(state); err == nil {
			t.Fatal("REVX should fail when x+y exceeds the stack length")
		}

		state = newStackState(1, 2)
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push BLKSWX bad x: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push BLKSWX bad y: %v", err)
		}
		if err := BLKSWX().Interpret(state); err == nil {
			t.Fatal("BLKSWX should fail when x+y exceeds the stack length")
		}
	})

	t.Run("OnlyXGuard", func(t *testing.T) {
		state := newStackState(1, 2, 3)
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push ONLYX count: %v", err)
		}
		if err := ONLYX().Interpret(state); err != nil {
			t.Fatalf("ONLYX failed: %v", err)
		}
		if got := popInts(t, state.Stack, 2); !equalInts(got, []int64{2, 1}) {
			t.Fatalf("unexpected ONLYX stack: %v", got)
		}

		state = newStackState(1)
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push ONLYX bad count: %v", err)
		}
		if err := ONLYX().Interpret(state); err == nil {
			t.Fatal("ONLYX should fail when asked to keep more values than present")
		}
	})
}
