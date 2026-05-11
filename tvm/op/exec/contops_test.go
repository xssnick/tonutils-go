package exec

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestCALLCCPushesCurrentContinuationAndSavesC0C1(t *testing.T) {
	state := newTestState()
	oldC0 := &testContinuation{name: "old_c0"}
	oldC1 := &testContinuation{name: "old_c1"}
	state.Reg.C[0] = oldC0
	state.Reg.C[1] = oldC1

	var captured *vm.OrdinaryContinuation
	body := &testContinuation{
		name: "body",
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
			return nil, nil
		},
	}

	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push body: %v", err)
	}

	if err := CALLCC().Interpret(state); err != nil {
		t.Fatalf("callcc failed: %v", err)
	}

	if captured == nil {
		t.Fatal("expected CALLCC to push current continuation")
	}
	if continuationName(t, captured.Data.Save.C[0]) != "old_c0" {
		t.Fatalf("expected saved c0 to be old_c0, got %#v", captured.Data.Save.C[0])
	}
	if continuationName(t, captured.Data.Save.C[1]) != "old_c1" {
		t.Fatalf("expected saved c1 to be old_c1, got %#v", captured.Data.Save.C[1])
	}
}

func TestCALLCCArgsCapturesRequestedReturnArity(t *testing.T) {
	state := newTestState()

	var captured *vm.OrdinaryContinuation
	body := &testContinuation{
		name: "body",
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
			if arg.Int64() != 55 {
				t.Fatalf("expected forwarded arg 55, got %s", arg.String())
			}
			return nil, nil
		},
	}

	if err := state.Stack.PushInt(big.NewInt(55)); err != nil {
		t.Fatalf("push arg: %v", err)
	}
	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push body: %v", err)
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
}

func TestCALLCCVarArgsUsesDynamicCounts(t *testing.T) {
	state := newTestState()

	var captured *vm.OrdinaryContinuation
	body := &testContinuation{
		name: "body",
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
			if arg.Int64() != 77 {
				t.Fatalf("expected forwarded arg 77, got %s", arg.String())
			}
			return nil, nil
		},
	}

	if err := state.Stack.PushInt(big.NewInt(77)); err != nil {
		t.Fatalf("push arg: %v", err)
	}
	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push body: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push params: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push retvals: %v", err)
	}

	if err := CALLCCVARARGS().Interpret(state); err != nil {
		t.Fatalf("callccvarargs failed: %v", err)
	}

	if captured == nil {
		t.Fatal("expected CALLCCVARARGS to push current continuation")
	}
	if captured.Data.NumArgs != 2 {
		t.Fatalf("expected saved return arity 2, got %d", captured.Data.NumArgs)
	}
}

func TestRETDATAPushesRemainingCodeSlice(t *testing.T) {
	state := newTestState()

	var recorded *cell.Slice
	state.Reg.C[0] = &testContinuation{
		name: "return_to_caller",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			sl, err := s.Stack.PopSlice()
			if err != nil {
				return nil, err
			}
			recorded = sl
			return nil, nil
		},
	}

	state.CurrentCode = cell.BeginCell().MustStoreUInt(0x73, 8).EndCell().BeginParse()

	if err := RETDATA().Interpret(state); err != nil {
		t.Fatalf("retdata failed: %v", err)
	}

	if recorded == nil {
		t.Fatal("expected RETDATA to push remaining code slice")
	}
	if got := recorded.MustLoadUInt(8); got != 0x73 {
		t.Fatalf("expected pushed code to start with 0x73, got %#x", got)
	}
}

func TestCONDSELCHKRejectsMismatchedTypes(t *testing.T) {
	state := &vm.State{Stack: vm.NewStack()}

	if err := state.Stack.PushBool(true); err != nil {
		t.Fatalf("push cond: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push x: %v", err)
	}
	if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
		t.Fatalf("push y: %v", err)
	}

	err := CONDSELCHK().Interpret(state)
	if err == nil {
		t.Fatal("expected type check error")
	}

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeTypeCheck {
		t.Fatalf("expected type check error, got %v", err)
	}
}

func TestCONDSELCHKReturnsSelectedValue(t *testing.T) {
	state := &vm.State{Stack: vm.NewStack()}

	if err := state.Stack.PushBool(true); err != nil {
		t.Fatalf("push cond: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
		t.Fatalf("push x: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
		t.Fatalf("push y: %v", err)
	}

	if err := CONDSELCHK().Interpret(state); err != nil {
		t.Fatalf("condselchk failed: %v", err)
	}

	got, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop result: %v", err)
	}
	if got.Int64() != 11 {
		t.Fatalf("expected selected value 11, got %s", got.String())
	}
}
