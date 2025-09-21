package exec

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type testContinuation struct {
	name   string
	onJump func(*vm.State) (vm.Continuation, error)
	data   *vm.ControlData
}

func (t *testContinuation) GetControlData() *vm.ControlData {
	return t.data
}

func (t *testContinuation) Jump(state *vm.State) (vm.Continuation, error) {
	if t.onJump != nil {
		return t.onJump(state)
	}
	return nil, nil
}

func (t *testContinuation) Copy() vm.Continuation {
	var dataCopy *vm.ControlData
	if t.data != nil {
		copied := t.data.Copy()
		dataCopy = &copied
	}
	return &testContinuation{
		name:   t.name,
		onJump: t.onJump,
		data:   dataCopy,
	}
}

func newTestState() *vm.State {
	st := &vm.State{
		Stack:       vm.NewStack(),
		CurrentCode: cell.BeginCell().EndCell().BeginParse(),
	}
	st.Reg.C[0] = &vm.QuitContinuation{ExitCode: 0}
	return st
}

func continuationName(t *testing.T, cont vm.Continuation) string {
	t.Helper()
	tc, ok := cont.(*testContinuation)
	if !ok {
		t.Fatalf("expected testContinuation, got %#v", cont)
	}
	return tc.name
}

func TestRepeatRunsBodyCountTimes(t *testing.T) {
	state := newTestState()
	original := &testContinuation{name: "after"}
	state.Reg.C[0] = original

	var iterations int
	body := &testContinuation{
		name: "body",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			iterations++
			return s.Reg.C[0], nil
		},
	}

	if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("push count: %v", err)
	}
	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push body: %v", err)
	}

	if err := REPEAT().Interpret(state); err != nil {
		t.Fatalf("repeat failed: %v", err)
	}

	if iterations != 3 {
		t.Fatalf("expected 3 iterations, got %d", iterations)
	}
	if continuationName(t, state.Reg.C[0]) != "after" {
		t.Fatalf("expected original continuation restored, got %#v", state.Reg.C[0])
	}
	if state.Stack.Len() != 0 {
		t.Fatalf("expected empty stack, got %d elements", state.Stack.Len())
	}
}

func TestRepeatSkipsNonPositive(t *testing.T) {
	cases := []int64{-2, -1, 0}
	for _, count := range cases {
		t.Run(big.NewInt(count).String(), func(t *testing.T) {
			state := newTestState()
			original := &testContinuation{name: "after"}
			state.Reg.C[0] = original

			bodyCalled := 0
			body := &testContinuation{
				name: "body",
				onJump: func(s *vm.State) (vm.Continuation, error) {
					bodyCalled++
					return nil, nil
				},
			}

			if err := state.Stack.PushInt(big.NewInt(count)); err != nil {
				t.Fatalf("push count: %v", err)
			}
			if err := state.Stack.PushContinuation(body); err != nil {
				t.Fatalf("push body: %v", err)
			}

			if err := REPEAT().Interpret(state); err != nil {
				t.Fatalf("repeat failed: %v", err)
			}

			if bodyCalled != 0 {
				t.Fatalf("expected body to be skipped, called %d times", bodyCalled)
			}
			if continuationName(t, state.Reg.C[0]) != "after" {
				t.Fatalf("expected c0 unchanged, got %#v", state.Reg.C[0])
			}
			if state.Stack.Len() != 0 {
				t.Fatalf("expected empty stack, got %d elements", state.Stack.Len())
			}
		})
	}
}

func TestRepeatEndSetsNextIteration(t *testing.T) {
	state := newTestState()
	state.CurrentCode = cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse()

	after := &testContinuation{name: "after"}
	state.Reg.C[0] = after

	if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("push count: %v", err)
	}

	if err := REPEATEND().Interpret(state); err != nil {
		t.Fatalf("repeatend failed: %v", err)
	}

	next, ok := state.Reg.C[0].(*vm.RepeatContinuation)
	if !ok {
		t.Fatalf("expected RepeatContinuation in c0, got %#v", state.Reg.C[0])
	}
	if next.Count != 2 {
		t.Fatalf("expected remaining count 2, got %d", next.Count)
	}
	if _, ok := next.Body.(*vm.OrdinaryContinuation); !ok {
		t.Fatalf("expected body to be OrdinaryContinuation, got %#v", next.Body)
	}
	if cont, ok := next.After.(*testContinuation); !ok || cont.name != "after" {
		t.Fatalf("expected after continuation copy, got %#v", next.After)
	}
	if state.Stack.Len() != 0 {
		t.Fatalf("expected empty stack, got %d elements", state.Stack.Len())
	}
}

func TestRepeatEndReturnOnZero(t *testing.T) {
	state := newTestState()
	state.CurrentCode = cell.BeginCell().MustStoreUInt(0xFF, 8).EndCell().BeginParse()

	var calls int
	original := &testContinuation{
		name: "after",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			calls++
			return nil, nil
		},
	}
	state.Reg.C[0] = original

	if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push count: %v", err)
	}

	if err := REPEATEND().Interpret(state); err != nil {
		t.Fatalf("repeatend failed: %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected return continuation to be invoked once, got %d", calls)
	}
	if _, ok := state.Reg.C[0].(*vm.QuitContinuation); !ok {
		t.Fatalf("expected c0 to hold quit continuation after return, got %#v", state.Reg.C[0])
	}
}

func TestUntilRunsUntilCondition(t *testing.T) {
	state := newTestState()
	original := &testContinuation{name: "after"}
	state.Reg.C[0] = original

	var iterations int
	body := &testContinuation{
		name: "body",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			iterations++
			if err := s.Stack.PushBool(iterations >= 3); err != nil {
				return nil, err
			}
			return s.Reg.C[0], nil
		},
	}

	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push body: %v", err)
	}

	if err := UNTIL().Interpret(state); err != nil {
		t.Fatalf("until failed: %v", err)
	}

	if iterations != 3 {
		t.Fatalf("expected 3 iterations, got %d", iterations)
	}
	if continuationName(t, state.Reg.C[0]) != "after" {
		t.Fatalf("expected original continuation restored, got %#v", state.Reg.C[0])
	}
	if state.Stack.Len() != 0 {
		t.Fatalf("expected empty stack, got %d elements", state.Stack.Len())
	}
}

func TestUntilEndInstallsContinuation(t *testing.T) {
	state := newTestState()
	state.CurrentCode = cell.BeginCell().MustStoreUInt(0x01, 2).EndCell().BeginParse()

	after := &testContinuation{name: "after"}
	state.Reg.C[0] = after

	if err := UNTILEND().Interpret(state); err != nil {
		t.Fatalf("untilend failed: %v", err)
	}

	cont, ok := state.Reg.C[0].(*vm.UntilContinuation)
	if !ok {
		t.Fatalf("expected UntilContinuation in c0, got %#v", state.Reg.C[0])
	}
	if body, ok := cont.Body.(*vm.OrdinaryContinuation); !ok || body == nil {
		t.Fatalf("expected body to be OrdinaryContinuation, got %#v", cont.Body)
	}
	if afterCopy, ok := cont.After.(*testContinuation); !ok || afterCopy.name != "after" {
		t.Fatalf("expected after continuation copy, got %#v", cont.After)
	}
}

func TestWhileRunsWhileCondition(t *testing.T) {
	state := newTestState()
	original := &testContinuation{name: "after"}
	state.Reg.C[0] = original

	var iterations int
	cond := &testContinuation{
		name: "cond",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			run := iterations < 3
			if err := s.Stack.PushBool(run); err != nil {
				return nil, err
			}
			return s.Reg.C[0], nil
		},
	}
	body := &testContinuation{
		name: "body",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			iterations++
			return s.Reg.C[0], nil
		},
	}

	if err := state.Stack.PushContinuation(cond); err != nil {
		t.Fatalf("push cond: %v", err)
	}
	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push body: %v", err)
	}

	if err := WHILE().Interpret(state); err != nil {
		t.Fatalf("while failed: %v", err)
	}

	if iterations != 3 {
		t.Fatalf("expected 3 iterations, got %d", iterations)
	}
	if continuationName(t, state.Reg.C[0]) != "after" {
		t.Fatalf("expected original continuation restored, got %#v", state.Reg.C[0])
	}
	if state.Stack.Len() != 0 {
		t.Fatalf("expected empty stack, got %d elements", state.Stack.Len())
	}
}

func TestWhileEndInstallsContinuation(t *testing.T) {
	state := newTestState()
	state.CurrentCode = cell.BeginCell().MustStoreUInt(0x0, 1).EndCell().BeginParse()

	after := &testContinuation{name: "after"}
	state.Reg.C[0] = after

	cond := &testContinuation{name: "cond"}
	if err := state.Stack.PushContinuation(cond); err != nil {
		t.Fatalf("push cond: %v", err)
	}

	if err := WHILEEND().Interpret(state); err != nil {
		t.Fatalf("whileend failed: %v", err)
	}

	cont, ok := state.Reg.C[0].(*vm.WhileContinuation)
	if !ok {
		t.Fatalf("expected WhileContinuation in c0, got %#v", state.Reg.C[0])
	}
	if body, ok := cont.Body.(*vm.OrdinaryContinuation); !ok || body == nil {
		t.Fatalf("expected body to be OrdinaryContinuation, got %#v", cont.Body)
	}
	if condCopy, ok := cont.Cond.(*testContinuation); !ok || condCopy.name != "cond" {
		t.Fatalf("expected cond copy, got %#v", cont.Cond)
	}
	if afterCopy, ok := cont.After.(*testContinuation); !ok || afterCopy.name != "after" {
		t.Fatalf("expected after continuation copy, got %#v", cont.After)
	}
}

func TestJumpX(t *testing.T) {
	state := newTestState()

	var called int
	target := &testContinuation{
		name: "target",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			called++
			return nil, nil
		},
	}

	if err := state.Stack.PushContinuation(target); err != nil {
		t.Fatalf("push target: %v", err)
	}

	if err := JMPX().Interpret(state); err != nil {
		t.Fatalf("jmpx failed: %v", err)
	}

	if called != 1 {
		t.Fatalf("expected target to be called once, got %d", called)
	}
	if state.Stack.Len() != 0 {
		t.Fatalf("expected empty stack, got %d elements", state.Stack.Len())
	}
}

func TestJumpXArgs(t *testing.T) {
	state := newTestState()

	var got []int64
	target := &testContinuation{
		name: "target",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			for s.Stack.Len() > 0 {
				val, err := s.Stack.PopInt()
				if err != nil {
					return nil, err
				}
				got = append(got, val.Int64())
			}
			return nil, nil
		},
	}

	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push arg1: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push arg2: %v", err)
	}
	if err := state.Stack.PushContinuation(target); err != nil {
		t.Fatalf("push target: %v", err)
	}

	if err := JMPXARGS(2).Interpret(state); err != nil {
		t.Fatalf("jmpxargs failed: %v", err)
	}

	expected := []int64{2, 1}
	if len(got) != len(expected) {
		t.Fatalf("expected %d args, got %d", len(expected), len(got))
	}
	for i, want := range expected {
		if got[i] != want {
			t.Fatalf("arg %d: expected %d, got %d", i, want, got[i])
		}
	}
}

func TestJumpXData(t *testing.T) {
	state := newTestState()
	code := cell.BeginCell().MustStoreUInt(0xC3, 8).MustStoreUInt(0x55, 8).EndCell().BeginParse()
	state.CurrentCode = code

	var pushedHash []byte
	target := &testContinuation{
		name: "target",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			slice, err := s.Stack.PopSlice()
			if err != nil {
				return nil, err
			}
			pushedHash = slice.MustToCell().Hash()
			return nil, nil
		},
	}

	if err := state.Stack.PushContinuation(target); err != nil {
		t.Fatalf("push target: %v", err)
	}

	if err := JMPXDATA().Interpret(state); err != nil {
		t.Fatalf("jmpxdata failed: %v", err)
	}

	if len(pushedHash) == 0 {
		t.Fatalf("expected data to be pushed")
	}
	if !bytes.Equal(pushedHash, code.MustToCell().Hash()) {
		t.Fatalf("expected pushed slice hash to match current code")
	}
}

func TestJumpXVarArgsFixed(t *testing.T) {
	state := newTestState()

	var got []int64
	target := &testContinuation{
		name: "target",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			for s.Stack.Len() > 0 {
				val, err := s.Stack.PopInt()
				if err != nil {
					return nil, err
				}
				got = append(got, val.Int64())
			}
			return nil, nil
		},
	}

	if err := state.Stack.PushInt(big.NewInt(7)); err != nil {
		t.Fatalf("push arg1: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(9)); err != nil {
		t.Fatalf("push arg2: %v", err)
	}
	if err := state.Stack.PushContinuation(target); err != nil {
		t.Fatalf("push target: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push count: %v", err)
	}

	if err := JMPXVARARGS().Interpret(state); err != nil {
		t.Fatalf("jmpxvarargs failed: %v", err)
	}

	expected := []int64{9}
	if len(got) != len(expected) {
		t.Fatalf("expected %d arg, got %d", len(expected), len(got))
	}
	for i, want := range expected {
		if got[i] != want {
			t.Fatalf("arg %d: expected %d, got %d", i, want, got[i])
		}
	}
}

func TestJumpXVarArgsAll(t *testing.T) {
	state := newTestState()

	var got []int64
	target := &testContinuation{
		name: "target",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			for s.Stack.Len() > 0 {
				val, err := s.Stack.PopInt()
				if err != nil {
					return nil, err
				}
				got = append(got, val.Int64())
			}
			return nil, nil
		},
	}

	if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("push arg1: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(5)); err != nil {
		t.Fatalf("push arg2: %v", err)
	}
	if err := state.Stack.PushContinuation(target); err != nil {
		t.Fatalf("push target: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("push count: %v", err)
	}

	if err := JMPXVARARGS().Interpret(state); err != nil {
		t.Fatalf("jmpxvarargs failed: %v", err)
	}

	expected := []int64{5, 3}
	if len(got) != len(expected) {
		t.Fatalf("expected %d args, got %d", len(expected), len(got))
	}
	for i, want := range expected {
		if got[i] != want {
			t.Fatalf("arg %d: expected %d, got %d", i, want, got[i])
		}
	}
}
