package tuple

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func assertTupleVMError(t *testing.T, err error, code int64) {
	t.Helper()
	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) {
		t.Fatalf("expected VMError %d, got %T (%v)", code, err, err)
	}
	if vmErr.Code != code {
		t.Fatalf("vm error code = %d, want %d", vmErr.Code, code)
	}
}

func TestTupleHelperErrorBranches(t *testing.T) {
	t.Run("ExecIndexQuietRejectsWrongTypeAndNegativeIndex", func(t *testing.T) {
		state := newState()
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push non-tuple: %v", err)
		}
		if err := execIndexQuiet(state, 0); err == nil {
			t.Fatal("expected type check for non-tuple input")
		}

		state = newState()
		mustPushTupleValue(t, state, big.NewInt(1))
		if err := execIndexQuiet(state, -1); err != nil {
			t.Fatalf("negative quiet index should return nil, got %v", err)
		}
		if got := popAny(t, state); got != nil {
			t.Fatalf("negative quiet index result = %v, want nil", got)
		}
	})

		t.Run("ExecSetIndexQuietRejectsBadIndexAndPreservesTupleOnNilFill", func(t *testing.T) {
			state := newState()
			if err := state.Stack.PushAny(nil); err != nil {
				t.Fatalf("push nil tuple: %v", err)
			}
			if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
				t.Fatalf("push value: %v", err)
			}
			if err := execSetIndexQuiet(state, 255); err == nil {
				t.Fatal("expected out-of-range set index to fail")
			} else {
			assertTupleVMError(t, err, vmerr.CodeRangeCheck)
		}

		state = newState()
		mustPushTupleValue(t, state, big.NewInt(1))
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push nil value: %v", err)
		}
		if err := execSetIndexQuiet(state, 3); err != nil {
			t.Fatalf("set nil beyond tuple len: %v", err)
		}
		tup := mustPopTupleValue(t, state)
		if tup.Len() != 1 {
			t.Fatalf("tuple len = %d, want 1", tup.Len())
		}
	})

	t.Run("ExecExplodeRejectsTupleLargerThanLimit", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state, big.NewInt(1), big.NewInt(2))
		if err := execExplode(state, 1); err == nil {
			t.Fatal("expected explode to fail when tuple exceeds max")
		}
	})
}

func TestTupleTailAndNullOpEdges(t *testing.T) {
	t.Run("LastAndTPopRejectEmptyTuple", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state)
		if err := LAST().Interpret(state); err == nil {
			t.Fatal("expected LAST to reject empty tuple")
		}

		state = newState()
		mustPushTupleValue(t, state)
		if err := TPOP().Interpret(state); err == nil {
			t.Fatal("expected TPOP to reject empty tuple")
		}
	})

	t.Run("TPushFailsWhenTupleGasCannotBeCharged", func(t *testing.T) {
		state := &vm.State{
			Stack: vm.NewStack(),
			Gas:   vm.Gas{},
		}
		mustPushTupleValue(t, state, big.NewInt(1))
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("push tail value: %v", err)
		}
		if err := TPUSH().Interpret(state); err == nil {
			t.Fatal("expected TPUSH to fail when tuple gas charge overflows")
		}
	})

	t.Run("MakeNullOpUnderflowAndNoopBranch", func(t *testing.T) {
		state := newState()
		err := makeNullOp("TEST", 0x6fa0, true, 1, 1).Interpret(state)
		assertTupleVMError(t, err, vmerr.CodeStackUnderflow)

		state = newState()
		pushInts(t, state, 5)
		if err = makeNullOp("TEST", 0x6fa0, true, 0, 1).Interpret(state); err != nil {
			t.Fatalf("null op noop branch: %v", err)
		}
		if got := popInt(t, state); got != 5 {
			t.Fatalf("null op should preserve top value, got %d", got)
		}
	})
}

func TestTupleInitRegistrationsInstantiateOps(t *testing.T) {
	if len(vm.List) == 0 {
		t.Fatal("vm.List should contain registered tuple ops")
	}

	instantiated := 0
	for _, getter := range vm.List {
		if getter == nil {
			continue
		}
		if op := getter(); op != nil {
			instantiated++
		}
	}
	if instantiated == 0 {
		t.Fatal("expected at least one op getter to instantiate an op")
	}
}
