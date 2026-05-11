package exec

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestBreakLoopOps(t *testing.T) {
	t.Run("repeatbrk_runs_and_restores_exits", func(t *testing.T) {
		state := newTestState()
		state.Reg.C[0] = &testContinuation{name: "after"}
		state.Reg.C[1] = &testContinuation{name: "alt"}

		iterations := 0
		body := &testContinuation{
			name: "body",
			onJump: func(s *vm.State) (vm.Continuation, error) {
				iterations++
				return s.Reg.C[0], nil
			},
		}

		if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("push repeat count: %v", err)
		}
		if err := state.Stack.PushContinuation(body); err != nil {
			t.Fatalf("push repeat body: %v", err)
		}

		if err := REPEATBRK().Interpret(state); err != nil {
			t.Fatalf("repeatbrk failed: %v", err)
		}

		if iterations != 3 {
			t.Fatalf("expected 3 repeat iterations, got %d", iterations)
		}
		if continuationName(t, state.Reg.C[0]) != "after" {
			t.Fatalf("expected c0 restored to after, got %#v", state.Reg.C[0])
		}
		if continuationName(t, state.Reg.C[1]) != "alt" {
			t.Fatalf("expected c1 restored to alt, got %#v", state.Reg.C[1])
		}
	})

	t.Run("repeatbrk_skips_non_positive", func(t *testing.T) {
		state := newTestState()
		calls := 0
		body := &testContinuation{
			name: "body",
			onJump: func(*vm.State) (vm.Continuation, error) {
				calls++
				return nil, nil
			},
		}

		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push repeat count: %v", err)
		}
		if err := state.Stack.PushContinuation(body); err != nil {
			t.Fatalf("push repeat body: %v", err)
		}

		if err := REPEATBRK().Interpret(state); err != nil {
			t.Fatalf("repeatbrk zero failed: %v", err)
		}
		if calls != 0 {
			t.Fatalf("expected body to be skipped, got %d calls", calls)
		}
	})

	t.Run("untilbrk_runs_until_true_and_restores_exits", func(t *testing.T) {
		state := newTestState()
		state.Reg.C[0] = &testContinuation{name: "after"}
		state.Reg.C[1] = &testContinuation{name: "alt"}

		iterations := 0
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
			t.Fatalf("push until body: %v", err)
		}

		if err := UNTILBRK().Interpret(state); err != nil {
			t.Fatalf("untilbrk failed: %v", err)
		}

		if iterations != 3 {
			t.Fatalf("expected 3 until iterations, got %d", iterations)
		}
		if continuationName(t, state.Reg.C[0]) != "after" {
			t.Fatalf("expected c0 restored to after, got %#v", state.Reg.C[0])
		}
		if continuationName(t, state.Reg.C[1]) != "alt" {
			t.Fatalf("expected c1 restored to alt, got %#v", state.Reg.C[1])
		}
	})

	t.Run("whilebrk_runs_and_restores_exits", func(t *testing.T) {
		state := newTestState()
		state.Reg.C[0] = &testContinuation{name: "after"}
		state.Reg.C[1] = &testContinuation{name: "alt"}

		iterations := 0
		cond := &testContinuation{
			name: "cond",
			onJump: func(s *vm.State) (vm.Continuation, error) {
				if err := s.Stack.PushBool(iterations < 3); err != nil {
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
			t.Fatalf("push while cond: %v", err)
		}
		if err := state.Stack.PushContinuation(body); err != nil {
			t.Fatalf("push while body: %v", err)
		}

		if err := WHILEBRK().Interpret(state); err != nil {
			t.Fatalf("whilebrk failed: %v", err)
		}

		if iterations != 3 {
			t.Fatalf("expected 3 while iterations, got %d", iterations)
		}
		if continuationName(t, state.Reg.C[0]) != "after" {
			t.Fatalf("expected c0 restored to after, got %#v", state.Reg.C[0])
		}
		if continuationName(t, state.Reg.C[1]) != "alt" {
			t.Fatalf("expected c1 restored to alt, got %#v", state.Reg.C[1])
		}
	})

	t.Run("repeatendbrk_installs_repeat_continuation", func(t *testing.T) {
		state := newTestState()
		state.CurrentCode = cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse()
		state.Reg.C[0] = &testContinuation{name: "after"}
		state.Reg.C[1] = &testContinuation{name: "alt"}

		if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("push repeatend count: %v", err)
		}
		if err := REPEATENDBRK().Interpret(state); err != nil {
			t.Fatalf("repeatendbrk failed: %v", err)
		}

		next, ok := state.Reg.C[0].(*vm.RepeatContinuation)
		if !ok {
			t.Fatalf("expected RepeatContinuation in c0, got %#v", state.Reg.C[0])
		}
		if next.Count != 2 {
			t.Fatalf("expected remaining repeat count 2, got %d", next.Count)
		}
		save := next.After.GetControlData().Save
		if continuationName(t, save.C[0]) != "after" {
			t.Fatalf("expected after continuation saved in c0, got %#v", save.C[0])
		}
		if continuationName(t, save.C[1]) != "alt" {
			t.Fatalf("expected alt continuation saved in c1, got %#v", save.C[1])
		}
	})

	t.Run("repeatendbrk_zero_returns_to_c0", func(t *testing.T) {
		state := newTestState()
		calls := 0
		state.Reg.C[0] = &testContinuation{
			name: "after",
			onJump: func(*vm.State) (vm.Continuation, error) {
				calls++
				return nil, nil
			},
		}

		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push repeatend zero count: %v", err)
		}
		if err := REPEATENDBRK().Interpret(state); err != nil {
			t.Fatalf("repeatendbrk zero failed: %v", err)
		}
		if calls != 1 {
			t.Fatalf("expected c0 continuation to run once, got %d", calls)
		}
		if _, ok := state.Reg.C[0].(*vm.QuitContinuation); !ok {
			t.Fatalf("expected c0 to be replaced by quit continuation, got %#v", state.Reg.C[0])
		}
	})

	t.Run("untilendbrk_installs_until_continuation", func(t *testing.T) {
		state := newTestState()
		state.CurrentCode = cell.BeginCell().MustStoreUInt(0x03, 2).EndCell().BeginParse()
		state.Reg.C[0] = &testContinuation{name: "after"}
		state.Reg.C[1] = &testContinuation{name: "alt"}

		if err := UNTILENDBRK().Interpret(state); err != nil {
			t.Fatalf("untilendbrk failed: %v", err)
		}

		next, ok := state.Reg.C[0].(*vm.UntilContinuation)
		if !ok {
			t.Fatalf("expected UntilContinuation in c0, got %#v", state.Reg.C[0])
		}
		if _, ok := next.Body.(*vm.OrdinaryContinuation); !ok {
			t.Fatalf("expected extracted body continuation, got %#v", next.Body)
		}
		save := next.After.GetControlData().Save
		if continuationName(t, save.C[0]) != "after" {
			t.Fatalf("expected after continuation saved in c0, got %#v", save.C[0])
		}
		if continuationName(t, save.C[1]) != "alt" {
			t.Fatalf("expected alt continuation saved in c1, got %#v", save.C[1])
		}
	})

	t.Run("whileendbrk_installs_while_continuation", func(t *testing.T) {
		state := newTestState()
		state.CurrentCode = cell.BeginCell().MustStoreUInt(0x01, 1).EndCell().BeginParse()
		state.Reg.C[0] = &testContinuation{name: "after"}
		state.Reg.C[1] = &testContinuation{name: "alt"}

		condCalls := 0
		cond := &testContinuation{
			name: "cond",
			onJump: func(*vm.State) (vm.Continuation, error) {
				condCalls++
				return nil, nil
			},
		}

		if err := state.Stack.PushContinuation(cond); err != nil {
			t.Fatalf("push whileend cond: %v", err)
		}
		if err := WHILEENDBRK().Interpret(state); err != nil {
			t.Fatalf("whileendbrk failed: %v", err)
		}

		if condCalls != 1 {
			t.Fatalf("expected condition to run once, got %d", condCalls)
		}
		next, ok := state.Reg.C[0].(*vm.WhileContinuation)
		if !ok {
			t.Fatalf("expected WhileContinuation in c0, got %#v", state.Reg.C[0])
		}
		if continuationName(t, next.Cond) != "cond" {
			t.Fatalf("expected condition continuation to be preserved, got %#v", next.Cond)
		}
		save := next.After.GetControlData().Save
		if continuationName(t, save.C[0]) != "after" {
			t.Fatalf("expected after continuation saved in c0, got %#v", save.C[0])
		}
		if continuationName(t, save.C[1]) != "alt" {
			t.Fatalf("expected alt continuation saved in c1, got %#v", save.C[1])
		}
	})

	t.Run("againbrk_captures_current_continuation_in_c1", func(t *testing.T) {
		state := newTestState()
		state.CurrentCode = cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell().BeginParse()
		state.Reg.C[0] = &testContinuation{name: "after"}
		state.Reg.C[1] = &testContinuation{name: "alt"}

		bodyCalls := 0
		body := &testContinuation{
			name: "body",
			onJump: func(*vm.State) (vm.Continuation, error) {
				bodyCalls++
				return nil, nil
			},
		}

		if err := state.Stack.PushContinuation(body); err != nil {
			t.Fatalf("push again body: %v", err)
		}
		if err := AGAINBRK().Interpret(state); err != nil {
			t.Fatalf("againbrk failed: %v", err)
		}

		if bodyCalls != 1 {
			t.Fatalf("expected body to run once, got %d", bodyCalls)
		}
		if _, ok := state.Reg.C[0].(*vm.AgainContinuation); !ok {
			t.Fatalf("expected AgainContinuation in c0, got %#v", state.Reg.C[0])
		}
		save := state.Reg.C[1].GetControlData().Save
		if continuationName(t, save.C[0]) != "after" {
			t.Fatalf("expected after continuation captured in c1, got %#v", save.C[0])
		}
		if continuationName(t, save.C[1]) != "alt" {
			t.Fatalf("expected alt continuation captured in c1, got %#v", save.C[1])
		}
	})

	t.Run("againendbrk_wraps_c0_and_restarts_current_code", func(t *testing.T) {
		state := newTestState()
		state.CurrentCode = cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell().BeginParse()
		state.Reg.C[0] = &testContinuation{name: "after"}
		state.Reg.C[1] = &testContinuation{name: "alt"}

		if err := AGAINENDBRK().Interpret(state); err != nil {
			t.Fatalf("againendbrk failed: %v", err)
		}

		if _, ok := state.Reg.C[0].(*vm.AgainContinuation); !ok {
			t.Fatalf("expected AgainContinuation in c0, got %#v", state.Reg.C[0])
		}
		argExt, ok := state.Reg.C[1].(*vm.ArgExtContinuation)
		if !ok {
			t.Fatalf("expected wrapped c0 in c1, got %#v", state.Reg.C[1])
		}
		if continuationName(t, argExt.Ext) != "after" {
			t.Fatalf("expected wrapped continuation to target original c0, got %#v", argExt.Ext)
		}
		if continuationName(t, argExt.Data.Save.C[1]) != "alt" {
			t.Fatalf("expected wrapped continuation to save original c1, got %#v", argExt.Data.Save.C[1])
		}
		if state.CurrentCode.MustLoadUInt(8) != 0xCC {
			t.Fatalf("expected current code to restart from extracted body")
		}
	})
}

func TestCALLCCARGSAndIFELSEBranches(t *testing.T) {
	t.Run("callccargs_forwards_args_and_current_continuation", func(t *testing.T) {
		state := newTestState()
		var captured *vm.OrdinaryContinuation
		body := &testContinuation{
			name: "callccargs",
			onJump: func(s *vm.State) (vm.Continuation, error) {
				cc, err := s.Stack.PopContinuation()
				if err != nil {
					return nil, err
				}
				var ok bool
				captured, ok = cc.(*vm.OrdinaryContinuation)
				if !ok {
					t.Fatalf("expected ordinary continuation, got %T", cc)
				}
				arg, err := s.Stack.PopIntFinite()
				if err != nil {
					return nil, err
				}
				if arg.Int64() != 41 {
					t.Fatalf("expected forwarded arg 41, got %s", arg.String())
				}
				return nil, nil
			},
		}

		if err := state.Stack.PushInt(big.NewInt(41)); err != nil {
			t.Fatalf("push callccargs arg: %v", err)
		}
		if err := state.Stack.PushContinuation(body); err != nil {
			t.Fatalf("push callccargs body: %v", err)
		}
		if err := CALLCCARGS(1, 2).Interpret(state); err != nil {
			t.Fatalf("callccargs failed: %v", err)
		}

		if captured == nil {
			t.Fatal("expected CALLCCARGS to push current continuation")
		}
		if captured.Data.NumArgs != 2 {
			t.Fatalf("expected saved return arity 2, got %d", captured.Data.NumArgs)
		}
	})

	t.Run("ifelse_false_branch_uses_alternate_continuation", func(t *testing.T) {
		state := newTestState()
		leftCalls := 0
		rightCalls := 0
		left := &testContinuation{name: "left", onJump: func(*vm.State) (vm.Continuation, error) { leftCalls++; return nil, nil }}
		right := &testContinuation{name: "right", onJump: func(*vm.State) (vm.Continuation, error) { rightCalls++; return nil, nil }}

		if err := state.Stack.PushBool(false); err != nil {
			t.Fatalf("push ifelse cond: %v", err)
		}
		if err := state.Stack.PushContinuation(left); err != nil {
			t.Fatalf("push ifelse left: %v", err)
		}
		if err := state.Stack.PushContinuation(right); err != nil {
			t.Fatalf("push ifelse right: %v", err)
		}

		if err := IFELSE().Interpret(state); err != nil {
			t.Fatalf("ifelse false failed: %v", err)
		}

		if leftCalls != 0 || rightCalls != 1 {
			t.Fatalf("unexpected IFELSE false-branch routing: left=%d right=%d", leftCalls, rightCalls)
		}
	})
}

func TestExecAdvancedOpRoundTrips(t *testing.T) {
	cases := []struct {
		name string
		op   vm.OP
		want string
	}{
		{name: "calldict_short", op: CALLDICT(7), want: "CALLDICT 7"},
		{name: "callxargs", op: CALLXARGS(2, 3), want: "CALLXARGS 2,3"},
		{name: "callxargsp", op: CALLXARGSP(5), want: "CALLXARGS 5,-1"},
		{name: "jmpxargs", op: JMPXARGS(6), want: "JMPXARGS 6"},
		{name: "returnargs", op: RETURNARGS(7), want: "RETURNARGS 7"},
		{name: "setcontargs", op: SETCONTARGS(2, 0), want: "SETCONTARGS 2,0"},
		{name: "setcontctrmany", op: SETCONTCTRMANY((1 << 1) | (1 << 4)), want: "SETCONTCTRMANY 19"},
		{name: "calldict_long", op: CALLDICT(0x1FF), want: "CALLDICT 511"},
		{name: "jmpdict", op: JMPDICT(12), want: "JMPDICT 12"},
		{name: "preparedict", op: PREPAREDICT(3), want: "PREPAREDICT 3"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			encoded := tt.op.Serialize().EndCell()
			decoded := tt.op
			if err := decoded.Deserialize(encoded.BeginParse()); err != nil {
				t.Fatalf("deserialize failed: %v", err)
			}
			if decoded.SerializeText() != tt.want {
				t.Fatalf("unexpected round-trip name: %q", decoded.SerializeText())
			}
		})
	}
}
