package exec

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func assertVMErrCode(t *testing.T, err error, code int64) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected VM error code %d", code)
	}
	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != code {
		t.Fatalf("expected VM error code %d, got %v", code, err)
	}
}

func TestRegisteredExecOpsInstantiate(t *testing.T) {
	if len(vm.List) == 0 {
		t.Fatal("expected exec package to register opcode getters")
	}

	for i, getter := range vm.List {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("registered getter %d panicked: %v", i, r)
				}
			}()
			if op := getter(); op == nil {
				t.Fatalf("registered getter %d returned nil", i)
			}
		}()
	}
}

func TestCallCCAdditionalCoverage(t *testing.T) {
	t.Run("GuardPaths", func(t *testing.T) {
		assertVMErrCode(t, CALLCC().Interpret(newTestState()), vmerr.CodeStackUnderflow)
		assertVMErrCode(t, CALLCCARGS(1, 0).Interpret(newTestState()), vmerr.CodeStackUnderflow)

		state := newTestState()
		if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(255)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}
		assertVMErrCode(t, CALLXVARARGS().Interpret(state), vmerr.CodeRangeCheck)

		state = newTestState()
		if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}
		assertVMErrCode(t, CALLCCVARARGS().Interpret(state), vmerr.CodeStackUnderflow)

		state = newTestState()
		if err := state.Stack.PushInt(big.NewInt(255)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}
		assertVMErrCode(t, RETVARARGS().Interpret(state), vmerr.CodeRangeCheck)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		src := CALLCCARGS(2, -1)
		dst := CALLCCARGS(0, 0)
		if err := dst.Deserialize(src.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); got != "CALLCCARGS 2,-1" {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 24 {
			t.Fatalf("unexpected bits: %d", got)
		}
	})
}

func TestSetContCtrAdditionalCoverage(t *testing.T) {
	t.Run("RoundTripAndTupleSave", func(t *testing.T) {
		src := SETCONTCTR(7)
		dst := SETCONTCTR(0)
		if err := dst.Deserialize(src.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); got != "c7 SETCONTCTR" {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 16 {
			t.Fatalf("unexpected bits: %d", got)
		}

		state := newTestState()
		if err := state.Stack.PushTuple(*tuple.NewTuple(big.NewInt(7))); err != nil {
			t.Fatalf("push tuple: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}
		if err := dst.Interpret(state); err != nil {
			t.Fatalf("setcontctr failed: %v", err)
		}
		gotCont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop continuation: %v", err)
		}
		if gotCont.GetControlData().Save.C7.Len() != 1 {
			t.Fatalf("expected c7 to be saved in continuation")
		}
	})

	t.Run("TypeAndRangeChecks", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push cell: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}
		assertVMErrCode(t, SETCONTCTR(0).Interpret(state), vmerr.CodeTypeCheck)

		state = newTestState()
		if err := state.Stack.PushInt(big.NewInt(6)); err != nil {
			t.Fatalf("push idx: %v", err)
		}
		assertVMErrCode(t, PUSHCTRX().Interpret(state), vmerr.CodeRangeCheck)

		state = newTestState()
		if err := state.Stack.PushInt(big.NewInt(6)); err != nil {
			t.Fatalf("push idx: %v", err)
		}
		assertVMErrCode(t, SETCONTCTRX().Interpret(state), vmerr.CodeRangeCheck)
	})
}

func TestJumpAndReturnGuardCoverage(t *testing.T) {
	t.Run("JmpxdataErrors", func(t *testing.T) {
		assertVMErrCode(t, JMPXDATA().Interpret(newTestState()), vmerr.CodeStackUnderflow)

		state := newTestState()
		state.CurrentCode = nil
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}
		assertVMErrCode(t, JMPXDATA().Interpret(state), vmerr.CodeTypeCheck)
	})

	t.Run("RetFamilyNoOpAndErrorBranches", func(t *testing.T) {
		state := newTestState()
		state.CurrentCode = nil
		assertVMErrCode(t, RETDATA().Interpret(state), vmerr.CodeTypeCheck)

		retCalls := 0
		state = newTestState()
		state.Reg.C[0] = &testContinuation{
			name: "ret",
			onJump: func(*vm.State) (vm.Continuation, error) {
				retCalls++
				return nil, nil
			},
		}
		if err := state.Stack.PushBool(false); err != nil {
			t.Fatalf("push IFRET cond: %v", err)
		}
		if err := IFRET().Interpret(state); err != nil {
			t.Fatalf("ifret false branch failed: %v", err)
		}
		if retCalls != 0 {
			t.Fatalf("expected IFRET false branch to skip return, got %d calls", retCalls)
		}

		altCalls := 0
		state = newTestState()
		state.Reg.C[1] = &testContinuation{
			name: "alt",
			onJump: func(*vm.State) (vm.Continuation, error) {
				altCalls++
				return nil, nil
			},
		}
		if err := state.Stack.PushBool(false); err != nil {
			t.Fatalf("push IFRETALT cond: %v", err)
		}
		if err := IFRETALT().Interpret(state); err != nil {
			t.Fatalf("ifretalt false branch failed: %v", err)
		}
		if err := state.Stack.PushBool(true); err != nil {
			t.Fatalf("push IFNOTRETALT cond: %v", err)
		}
		if err := IFNOTRETALT().Interpret(state); err != nil {
			t.Fatalf("ifnotretalt true branch failed: %v", err)
		}
		if altCalls != 0 {
			t.Fatalf("expected alternate returns to be skipped, got %d calls", altCalls)
		}
	})
}

func TestControlRegisterHelperBranches(t *testing.T) {
	t.Run("IndexValidation", func(t *testing.T) {
		valid := []int{0, 1, 2, 3, 4, 5, 7}
		for _, idx := range valid {
			if !validControlRegisterIndex(idx) {
				t.Fatalf("expected index %d to be valid", idx)
			}
		}
		for _, idx := range []int{-1, 6, 8} {
			if validControlRegisterIndex(idx) {
				t.Fatalf("expected index %d to be invalid", idx)
			}
		}
	})

	t.Run("CloneControlRegisterValue", func(t *testing.T) {
		origCont := &testContinuation{name: "orig"}
		clonedCont, ok := cloneControlRegisterValue(origCont).(vm.Continuation)
		if !ok {
			t.Fatalf("expected continuation clone")
		}
		if clonedCont == origCont {
			t.Fatal("expected continuation clone to be a copy")
		}

		origTuple := *tuple.NewTuple(big.NewInt(9))
		clonedTuple, ok := cloneControlRegisterValue(origTuple).(tuple.Tuple)
		if !ok || clonedTuple.Len() != 1 {
			t.Fatalf("expected tuple clone")
		}

		var nilCont vm.Continuation = (*testContinuation)(nil)
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected typed nil continuation clone to panic")
				}
			}()
			_ = cloneControlRegisterValue(nilCont)
		}()

		val := big.NewInt(123)
		if got := cloneControlRegisterValue(val); got != val {
			t.Fatal("expected default clone branch to return original value")
		}
	})

	t.Run("PushCtrClonesContinuation", func(t *testing.T) {
		state := newTestState()
		state.Reg.C[1] = &testContinuation{name: "alt"}
		if err := PUSHCTR(1).Interpret(state); err != nil {
			t.Fatalf("pushctr failed: %v", err)
		}
		got, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop continuation: %v", err)
		}
		if got == state.Reg.C[1] {
			t.Fatal("expected PUSHCTR to clone continuation values")
		}
	})
}
