package tuple

import (
	"errors"
	"math/big"
	"testing"

	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
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

func assertTupleVMErrorText(t *testing.T, err error, code int64, msg string) {
	t.Helper()
	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) {
		t.Fatalf("expected VMError %d, got %T (%v)", code, err, err)
	}
	if vmErr.Code != code || vmErr.Msg != msg {
		t.Fatalf("vm error = (%d, %q), want (%d, %q)", vmErr.Code, vmErr.Msg, code, msg)
	}
}

func assertTupleStackLen(t *testing.T, state *vm.State, want int) {
	t.Helper()
	if got := state.Stack.Len(); got != want {
		t.Fatalf("stack len = %d, want %d", got, want)
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

func TestTupleVarOperandErrorStackEffects(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, state *vm.State)
		run     func(state *vm.State) error
		code    int64
		wantLen int
	}{
		{
			name: "IndexVarShortStackKeepsStack",
			setup: func(t *testing.T, state *vm.State) {
				pushInts(t, state, 300)
			},
			run:     func(state *vm.State) error { return INDEXVAR().Interpret(state) },
			code:    vmerr.CodeStackUnderflow,
			wantLen: 1,
		},
		{
			name: "IndexVarRangeConsumesOnlyIndex",
			setup: func(t *testing.T, state *vm.State) {
				mustPushTupleValue(t, state, big.NewInt(1))
				pushInts(t, state, 255)
			},
			run:     func(state *vm.State) error { return INDEXVAR().Interpret(state) },
			code:    vmerr.CodeRangeCheck,
			wantLen: 1,
		},
		{
			name: "IndexVarQTypeConsumesOnlyIndex",
			setup: func(t *testing.T, state *vm.State) {
				mustPushTupleValue(t, state, big.NewInt(1))
				if err := state.Stack.PushAny(nil); err != nil {
					t.Fatalf("push bad index: %v", err)
				}
			},
			run:     func(state *vm.State) error { return INDEXVARQ().Interpret(state) },
			code:    vmerr.CodeTypeCheck,
			wantLen: 1,
		},
		{
			name: "SetIndexVarShortStackKeepsStack",
			setup: func(t *testing.T, state *vm.State) {
				pushInts(t, state, 7, 0)
			},
			run:     func(state *vm.State) error { return SETINDEXVAR().Interpret(state) },
			code:    vmerr.CodeStackUnderflow,
			wantLen: 2,
		},
		{
			name: "SetIndexVarRangeConsumesOnlyIndex",
			setup: func(t *testing.T, state *vm.State) {
				mustPushTupleValue(t, state, big.NewInt(1))
				pushInts(t, state, 5, 255)
			},
			run:     func(state *vm.State) error { return SETINDEXVAR().Interpret(state) },
			code:    vmerr.CodeRangeCheck,
			wantLen: 2,
		},
		{
			name: "SetIndexVarQTypeConsumesOnlyIndex",
			setup: func(t *testing.T, state *vm.State) {
				mustPushTupleValue(t, state, big.NewInt(1))
				pushInts(t, state, 5)
				if err := state.Stack.PushAny(nil); err != nil {
					t.Fatalf("push bad index: %v", err)
				}
			},
			run:     func(state *vm.State) error { return SETINDEXVARQ().Interpret(state) },
			code:    vmerr.CodeTypeCheck,
			wantLen: 2,
		},
		{
			name: "ExplodeVarShortStackKeepsStack",
			setup: func(t *testing.T, state *vm.State) {
				pushInts(t, state, 3)
			},
			run:     func(state *vm.State) error { return EXPLODEVAR().Interpret(state) },
			code:    vmerr.CodeStackUnderflow,
			wantLen: 1,
		},
		{
			name: "ExplodeVarRangeConsumesOnlyMax",
			setup: func(t *testing.T, state *vm.State) {
				mustPushTupleValue(t, state, big.NewInt(1))
				pushInts(t, state, 256)
			},
			run:     func(state *vm.State) error { return EXPLODEVAR().Interpret(state) },
			code:    vmerr.CodeRangeCheck,
			wantLen: 1,
		},
		{
			name: "UntupleVarTypeConsumesOnlyCount",
			setup: func(t *testing.T, state *vm.State) {
				mustPushTupleValue(t, state, big.NewInt(1))
				if err := state.Stack.PushAny(nil); err != nil {
					t.Fatalf("push bad count: %v", err)
				}
			},
			run:     func(state *vm.State) error { return UNTUPLEVAR().Interpret(state) },
			code:    vmerr.CodeTypeCheck,
			wantLen: 1,
		},
		{
			name: "UnpackFirstVarShortStackKeepsStack",
			setup: func(t *testing.T, state *vm.State) {
				pushInts(t, state, 1)
			},
			run:     func(state *vm.State) error { return UNPACKFIRSTVAR().Interpret(state) },
			code:    vmerr.CodeStackUnderflow,
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newState()
			tt.setup(t, state)
			assertTupleVMError(t, tt.run(state), tt.code)
			assertTupleStackLen(t, state, tt.wantLen)
		})
	}
}

func TestTupleSetIndexErrorStackEffects(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, state *vm.State)
		run     func(state *vm.State) error
		code    int64
		wantLen int
	}{
		{
			name: "SetIndexOutOfRangeConsumesOperands",
			setup: func(t *testing.T, state *vm.State) {
				mustPushTupleValue(t, state, big.NewInt(1))
				pushInts(t, state, 9)
			},
			run:     func(state *vm.State) error { return SETINDEX(5).Interpret(state) },
			code:    vmerr.CodeRangeCheck,
			wantLen: 0,
		},
		{
			name: "SetIndexNonTupleConsumesOperands",
			setup: func(t *testing.T, state *vm.State) {
				pushInts(t, state, 1, 2)
			},
			run:     func(state *vm.State) error { return SETINDEX(0).Interpret(state) },
			code:    vmerr.CodeTypeCheck,
			wantLen: 0,
		},
		{
			name: "SetIndexQuietSingleValueUnderflowConsumesValue",
			setup: func(t *testing.T, state *vm.State) {
				pushInts(t, state, 7)
			},
			run:     func(state *vm.State) error { return SETINDEXQ(0).Interpret(state) },
			code:    vmerr.CodeStackUnderflow,
			wantLen: 0,
		},
		{
			name: "SetIndexQuietNonTupleConsumesOperands",
			setup: func(t *testing.T, state *vm.State) {
				pushInts(t, state, 1, 2)
			},
			run:     func(state *vm.State) error { return SETINDEXQ(0).Interpret(state) },
			code:    vmerr.CodeTypeCheck,
			wantLen: 0,
		},
		{
			name: "SetIndexQuietBadIndexConsumesOperands",
			setup: func(t *testing.T, state *vm.State) {
				if err := state.Stack.PushAny(nil); err != nil {
					t.Fatalf("push nil tuple: %v", err)
				}
				pushInts(t, state, 1)
			},
			run:     func(state *vm.State) error { return execSetIndexQuiet(state, 255) },
			code:    vmerr.CodeRangeCheck,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newState()
			tt.setup(t, state)
			assertTupleVMError(t, tt.run(state), tt.code)
			assertTupleStackLen(t, state, tt.wantLen)
		})
	}
}

func TestTupleNestedIndexAndGasErrorStackEffects(t *testing.T) {
	t.Run("Index2EmptyStackUnderflow", func(t *testing.T) {
		state := newState()
		assertTupleVMError(t, INDEX2(0, 0).Interpret(state), vmerr.CodeStackUnderflow)
		assertTupleStackLen(t, state, 0)
	})

	t.Run("Index2FinalRangeConsumesOuterTuple", func(t *testing.T) {
		state := newState()
		inner := tuplepkg.NewTupleValue(big.NewInt(10))
		mustPushTupleValue(t, state, inner)

		assertTupleVMError(t, INDEX2(0, 1).Interpret(state), vmerr.CodeRangeCheck)
		assertTupleStackLen(t, state, 0)
	})

	t.Run("Index3RejectsSecondIntermediateNonTuple", func(t *testing.T) {
		state := newState()
		inner := tuplepkg.NewTupleValue(big.NewInt(10))
		mustPushTupleValue(t, state, inner)

		assertTupleVMError(t, INDEX3(0, 0, 0).Interpret(state), vmerr.CodeTypeCheck)
		assertTupleStackLen(t, state, 0)
	})

	t.Run("Index3FinalRangeConsumesOuterTuple", func(t *testing.T) {
		state := newState()
		inner := tuplepkg.NewTupleValue(big.NewInt(10))
		middle := tuplepkg.NewTupleValue(inner)
		mustPushTupleValue(t, state, middle)

		assertTupleVMError(t, INDEX3(0, 0, 1).Interpret(state), vmerr.CodeRangeCheck)
		assertTupleStackLen(t, state, 0)
	})

	t.Run("ExplodeOutOfGasLeavesPushedItems", func(t *testing.T) {
		state := &vm.State{Stack: vm.NewStack(), Gas: vm.Gas{}}
		mustPushTupleValue(t, state, big.NewInt(4), big.NewInt(5))

		assertTupleVMError(t, EXPLODE(2).Interpret(state), vmerr.CodeOutOfGas)
		assertTupleStackLen(t, state, 2)
		if got := popInt(t, state); got != 5 {
			t.Fatalf("top exploded item = %d, want 5", got)
		}
		if got := popInt(t, state); got != 4 {
			t.Fatalf("bottom exploded item = %d, want 4", got)
		}
	})

	t.Run("UntupleOutOfGasLeavesPushedItems", func(t *testing.T) {
		state := &vm.State{Stack: vm.NewStack(), Gas: vm.Gas{}}
		mustPushTupleValue(t, state, big.NewInt(6), big.NewInt(7))

		assertTupleVMError(t, UNTUPLE(2).Interpret(state), vmerr.CodeOutOfGas)
		assertTupleStackLen(t, state, 2)
		if got := popInt(t, state); got != 7 {
			t.Fatalf("top untupled item = %d, want 7", got)
		}
		if got := popInt(t, state); got != 6 {
			t.Fatalf("bottom untupled item = %d, want 6", got)
		}
	})

	t.Run("TuplePredicatesUnderflow", func(t *testing.T) {
		state := newState()
		assertTupleVMError(t, QTLEN().Interpret(state), vmerr.CodeStackUnderflow)
		assertTupleStackLen(t, state, 0)

		state = newState()
		assertTupleVMError(t, ISTUPLE().Interpret(state), vmerr.CodeStackUnderflow)
		assertTupleStackLen(t, state, 0)
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

func TestTupleErrorTextParity(t *testing.T) {
	t.Run("PopTupleRangeRejectsNonTupleLikeCPP", func(t *testing.T) {
		state := newState()
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push non-tuple: %v", err)
		}
		assertTupleVMErrorText(t, INDEX(0).Interpret(state), vmerr.CodeTypeCheck, "not a tuple of valid size")
	})

	t.Run("TupleIndexUsesCPPRangeText", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state)
		assertTupleVMErrorText(t, INDEX(0).Interpret(state), vmerr.CodeRangeCheck, "tuple index out of range")
	})

	t.Run("PopMaybeTupleRangeRejectsNonTupleLikeCPP", func(t *testing.T) {
		state := newState()
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push non-tuple: %v", err)
		}
		assertTupleVMErrorText(t, INDEXQ(0).Interpret(state), vmerr.CodeTypeCheck, "not a tuple of valid size")
	})

	t.Run("IndexMultiOversizedIntermediateUsesCPPText", func(t *testing.T) {
		state := newState()
		oversized := tuplepkg.NewTupleSized(256)
		if err := state.Stack.PushTuple(tuplepkg.NewTupleValue(oversized)); err != nil {
			t.Fatalf("push tuple: %v", err)
		}
		assertTupleVMErrorText(t, INDEX2(0, 0).Interpret(state), vmerr.CodeTypeCheck, "intermediate value is not a tuple")
	})

	t.Run("NullOpRejectsNonIntegerLikeCPP", func(t *testing.T) {
		state := newState()
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("push null: %v", err)
		}
		assertTupleVMErrorText(t, NULLSWAPIF().Interpret(state), vmerr.CodeTypeCheck, "not an integer")
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
