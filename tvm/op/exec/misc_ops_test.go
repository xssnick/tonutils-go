package exec

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestAgainOps(t *testing.T) {
	state := newTestState()
	calls := 0
	body := &testContinuation{
		name: "body",
		onJump: func(*vm.State) (vm.Continuation, error) {
			calls++
			return nil, nil
		},
	}

	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push body: %v", err)
	}
	if err := AGAIN().Interpret(state); err != nil {
		t.Fatalf("again failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected body to run once, got %d", calls)
	}
	if _, ok := state.Reg.C[0].(*vm.AgainContinuation); !ok {
		t.Fatalf("expected c0 to hold AgainContinuation, got %T", state.Reg.C[0])
	}

	state = newTestState()
	state.CurrentCode = cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().BeginParse()
	if err := AGAINEND().Interpret(state); err != nil {
		t.Fatalf("againend failed: %v", err)
	}
	if _, ok := state.Reg.C[0].(*vm.AgainContinuation); !ok {
		t.Fatalf("expected c0 to hold AgainContinuation after AGAINEND, got %T", state.Reg.C[0])
	}
	if state.CurrentCode.MustLoadUInt(8) != 0xAB {
		t.Fatalf("expected current code to be preserved")
	}
}

func TestCompositionOps(t *testing.T) {
	composeCases := []struct {
		name string
		op   vm.OP
		c0   bool
		c1   bool
	}{
		{name: "boolor", op: BOOLOR(), c0: false, c1: true},
		{name: "booland", op: BOOLAND(), c0: true, c1: false},
		{name: "composboth", op: COMPOSBOTH(), c0: true, c1: true},
	}

	for _, tt := range composeCases {
		t.Run(tt.name, func(t *testing.T) {
			state := newTestState()
			base := &testContinuation{name: "base"}
			val := &testContinuation{name: "val"}

			if err := state.Stack.PushContinuation(base); err != nil {
				t.Fatalf("push base: %v", err)
			}
			if err := state.Stack.PushContinuation(val); err != nil {
				t.Fatalf("push val: %v", err)
			}
			if err := tt.op.Interpret(state); err != nil {
				t.Fatalf("interpret failed: %v", err)
			}

			got, err := state.Stack.PopContinuation()
			if err != nil {
				t.Fatalf("pop continuation: %v", err)
			}
			data := got.GetControlData()
			if data == nil {
				t.Fatal("expected control data on composed continuation")
			}
			if (data.Save.C[0] != nil) != tt.c0 {
				t.Fatalf("unexpected c0 save presence: %#v", data.Save.C[0])
			}
			if (data.Save.C[1] != nil) != tt.c1 {
				t.Fatalf("unexpected c1 save presence: %#v", data.Save.C[1])
			}
		})
	}

	state := newTestState()
	oldC0 := &testContinuation{name: "old_c0"}
	oldC1 := &testContinuation{name: "old_c1"}
	state.Reg.C[0], state.Reg.C[1] = oldC0, oldC1

	exit := &testContinuation{name: "exit"}
	if err := state.Stack.PushContinuation(exit); err != nil {
		t.Fatalf("push exit: %v", err)
	}
	if err := ATEXIT().Interpret(state); err != nil {
		t.Fatalf("atexit failed: %v", err)
	}
	if state.Reg.C[0].GetControlData().Save.C[0] != oldC0 {
		t.Fatalf("expected old c0 to be saved")
	}

	alt := &testContinuation{name: "alt"}
	if err := state.Stack.PushContinuation(alt); err != nil {
		t.Fatalf("push alt: %v", err)
	}
	if err := ATEXITALT().Interpret(state); err != nil {
		t.Fatalf("atexitalt failed: %v", err)
	}
	if state.Reg.C[1].GetControlData().Save.C[1] != oldC1 {
		t.Fatalf("expected old c1 to be saved")
	}

	setAlt := &testContinuation{name: "set_alt"}
	if err := state.Stack.PushContinuation(setAlt); err != nil {
		t.Fatalf("push set alt: %v", err)
	}
	if err := SETEXITALT().Interpret(state); err != nil {
		t.Fatalf("setexitalt failed: %v", err)
	}
	if state.Reg.C[1].GetControlData().Save.C[0] == nil || state.Reg.C[1].GetControlData().Save.C[1] == nil {
		t.Fatalf("expected both exits to be saved")
	}

	thenRet := &testContinuation{name: "then_ret"}
	if err := state.Stack.PushContinuation(thenRet); err != nil {
		t.Fatalf("push thenret: %v", err)
	}
	if err := THENRET().Interpret(state); err != nil {
		t.Fatalf("thenret failed: %v", err)
	}
	got, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop thenret cont: %v", err)
	}
	if got.GetControlData().Save.C[0] == nil {
		t.Fatalf("expected c0 to be captured by THENRET")
	}

	thenRetAlt := &testContinuation{name: "then_ret_alt"}
	if err := state.Stack.PushContinuation(thenRetAlt); err != nil {
		t.Fatalf("push thenretalt: %v", err)
	}
	if err := THENRETALT().Interpret(state); err != nil {
		t.Fatalf("thenretalt failed: %v", err)
	}
	got, err = state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop thenretalt cont: %v", err)
	}
	if got.GetControlData().Save.C[0] == nil {
		t.Fatalf("expected alt exit to be captured")
	}

	state = newTestState()
	state.Reg.C[0] = &testContinuation{name: "ret"}
	state.Reg.C[1] = &testContinuation{name: "alt"}
	if err := INVERT().Interpret(state); err != nil {
		t.Fatalf("invert failed: %v", err)
	}
	if continuationName(t, state.Reg.C[0]) != "alt" || continuationName(t, state.Reg.C[1]) != "ret" {
		t.Fatalf("expected exits to be swapped")
	}

	calls := 0
	state = newTestState()
	cond := &testContinuation{
		name: "bool_eval",
		onJump: func(*vm.State) (vm.Continuation, error) {
			calls++
			return nil, nil
		},
	}
	if err := state.Stack.PushContinuation(cond); err != nil {
		t.Fatalf("push booleval cont: %v", err)
	}
	if err := BOOLEVAL().Interpret(state); err != nil {
		t.Fatalf("booleval failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected booleval continuation to run once, got %d", calls)
	}
	if _, ok := state.Reg.C[0].(*vm.PushIntContinuation); !ok {
		t.Fatalf("expected c0 to hold PushIntContinuation, got %T", state.Reg.C[0])
	}
	if _, ok := state.Reg.C[1].(*vm.PushIntContinuation); !ok {
		t.Fatalf("expected c1 to hold PushIntContinuation, got %T", state.Reg.C[1])
	}
}

func TestCallIfAndExecuteOps(t *testing.T) {
	runCond := func(t *testing.T, op vm.OP, cond bool, expectCalls int) {
		t.Helper()
		state := newTestState()
		calls := 0
		body := &testContinuation{
			name: "body",
			onJump: func(*vm.State) (vm.Continuation, error) {
				calls++
				return nil, nil
			},
		}
		if err := state.Stack.PushBool(cond); err != nil {
			t.Fatalf("push cond: %v", err)
		}
		if err := state.Stack.PushContinuation(body); err != nil {
			t.Fatalf("push body: %v", err)
		}
		if err := op.Interpret(state); err != nil {
			t.Fatalf("interpret failed: %v", err)
		}
		if calls != expectCalls {
			t.Fatalf("expected %d calls, got %d", expectCalls, calls)
		}
	}

	runCond(t, IF(), true, 1)
	runCond(t, IF(), false, 0)
	runCond(t, IFNOT(), false, 1)
	runCond(t, IFNOT(), true, 0)
	runCond(t, IFJMP(), true, 1)
	runCond(t, IFJMP(), false, 0)
	runCond(t, IFNOTJMP(), false, 1)
	runCond(t, IFNOTJMP(), true, 0)

	state := newTestState()
	calls := 0
	body := &testContinuation{
		name: "exec",
		onJump: func(*vm.State) (vm.Continuation, error) {
			calls++
			return nil, nil
		},
	}
	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push execute body: %v", err)
	}
	if err := EXECUTE().Interpret(state); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected execute body to run once, got %d", calls)
	}

	state = newTestState()
	leftCalls := 0
	rightCalls := 0
	left := &testContinuation{name: "left", onJump: func(*vm.State) (vm.Continuation, error) { leftCalls++; return nil, nil }}
	right := &testContinuation{name: "right", onJump: func(*vm.State) (vm.Continuation, error) { rightCalls++; return nil, nil }}
	if err := state.Stack.PushBool(true); err != nil {
		t.Fatalf("push ifelse cond: %v", err)
	}
	if err := state.Stack.PushContinuation(left); err != nil {
		t.Fatalf("push ifelse left: %v", err)
	}
	if err := state.Stack.PushContinuation(right); err != nil {
		t.Fatalf("push ifelse right: %v", err)
	}
	if err := IFELSE().Interpret(state); err != nil {
		t.Fatalf("ifelse failed: %v", err)
	}
	if leftCalls != 1 || rightCalls != 0 {
		t.Fatalf("unexpected IFELSE routing: left=%d right=%d", leftCalls, rightCalls)
	}
}

func TestReturnAndControlOps(t *testing.T) {
	state := newTestState()
	retCalls := 0
	state.Reg.C[0] = &testContinuation{
		name: "ret",
		onJump: func(*vm.State) (vm.Continuation, error) {
			retCalls++
			return nil, nil
		},
	}
	if err := RET().Interpret(state); err != nil {
		t.Fatalf("ret failed: %v", err)
	}
	if retCalls != 1 {
		t.Fatalf("expected RET to jump once, got %d", retCalls)
	}

	state = newTestState()
	state.Reg.C[1] = &testContinuation{
		name: "alt",
		onJump: func(*vm.State) (vm.Continuation, error) {
			retCalls++
			return nil, nil
		},
	}
	before := retCalls
	if err := RETALT().Interpret(state); err != nil {
		t.Fatalf("retalt failed: %v", err)
	}
	if retCalls != before+1 {
		t.Fatalf("expected RETALT to jump once")
	}

	state = newTestState()
	gotArg := int64(0)
	state.Reg.C[0] = &testContinuation{
		name: "retargs",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			val, err := s.Stack.PopIntFinite()
			if err != nil {
				return nil, err
			}
			gotArg = val.Int64()
			return nil, nil
		},
	}
	if err := state.Stack.PushInt(big.NewInt(55)); err != nil {
		t.Fatalf("push return arg: %v", err)
	}
	if err := RETARGS(1).Interpret(state); err != nil {
		t.Fatalf("retargs failed: %v", err)
	}
	if gotArg != 55 {
		t.Fatalf("expected RETARGS to pass 55, got %d", gotArg)
	}

	state = newTestState()
	trueCalls := 0
	falseCalls := 0
	state.Reg.C[0] = &testContinuation{name: "ret", onJump: func(*vm.State) (vm.Continuation, error) { trueCalls++; return nil, nil }}
	state.Reg.C[1] = &testContinuation{name: "alt", onJump: func(*vm.State) (vm.Continuation, error) { falseCalls++; return nil, nil }}
	if err := state.Stack.PushBool(true); err != nil {
		t.Fatalf("push true: %v", err)
	}
	if err := RETBOOL().Interpret(state); err != nil {
		t.Fatalf("retbool true failed: %v", err)
	}
	if err := state.Stack.PushBool(false); err != nil {
		t.Fatalf("push false: %v", err)
	}
	if err := RETBOOL().Interpret(state); err != nil {
		t.Fatalf("retbool false failed: %v", err)
	}
	if trueCalls != 1 || falseCalls != 1 {
		t.Fatalf("unexpected retbool routing: true=%d false=%d", trueCalls, falseCalls)
	}

	state = newTestState()
	state.Reg.C[0] = &testContinuation{name: "ifret", onJump: func(*vm.State) (vm.Continuation, error) { trueCalls++; return nil, nil }}
	if err := state.Stack.PushBool(true); err != nil {
		t.Fatalf("push ifret cond: %v", err)
	}
	if err := IFRET().Interpret(state); err != nil {
		t.Fatalf("ifret failed: %v", err)
	}

	state = newTestState()
	state.Reg.C[1] = &testContinuation{name: "ifretalt", onJump: func(*vm.State) (vm.Continuation, error) { falseCalls++; return nil, nil }}
	if err := state.Stack.PushBool(true); err != nil {
		t.Fatalf("push ifretalt cond: %v", err)
	}
	if err := IFRETALT().Interpret(state); err != nil {
		t.Fatalf("ifretalt failed: %v", err)
	}

	state = newTestState()
	state.Reg.C[1] = &testContinuation{name: "ifnotretalt", onJump: func(*vm.State) (vm.Continuation, error) { falseCalls++; return nil, nil }}
	if err := state.Stack.PushBool(false); err != nil {
		t.Fatalf("push ifnotretalt cond: %v", err)
	}
	if err := IFNOTRETALT().Interpret(state); err != nil {
		t.Fatalf("ifnotretalt failed: %v", err)
	}
	if falseCalls < 3 {
		t.Fatalf("expected alternate returns to be used, got %d calls", falseCalls)
	}

	state = newTestState()
	state.Reg.C[0] = &testContinuation{name: "ifnotret", onJump: func(*vm.State) (vm.Continuation, error) { trueCalls++; return nil, nil }}
	if err := state.Stack.PushBool(false); err != nil {
		t.Fatalf("push ifnotret cond: %v", err)
	}
	if err := IFNOTRET().Interpret(state); err != nil {
		t.Fatalf("ifnotret failed: %v", err)
	}

	state = newTestState()
	state.Reg.C[0] = &testContinuation{name: "ret"}
	state.Reg.C[1] = &testContinuation{name: "alt"}
	if err := SAMEALT().Interpret(state); err != nil {
		t.Fatalf("samealt failed: %v", err)
	}
	if continuationName(t, state.Reg.C[1]) != "ret" || state.Reg.C[0] == state.Reg.C[1] {
		t.Fatalf("expected c1 to be a copy of c0")
	}

	state = newTestState()
	state.Reg.C[0] = &testContinuation{name: "ret"}
	state.Reg.C[1] = &testContinuation{name: "alt"}
	if err := SAMEALTSAVE().Interpret(state); err != nil {
		t.Fatalf("samealtsave failed: %v", err)
	}
	if state.Reg.C[0].GetControlData().Save.C[1] == nil || state.Reg.C[1].GetControlData() == nil {
		t.Fatalf("expected alt continuation to be saved")
	}

	state = newTestState()
	state.Reg.C[0] = &testContinuation{name: "ret"}
	state.Reg.D[0] = cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	if err := SAVECTR(4).Interpret(state); err != nil {
		t.Fatalf("savectr failed: %v", err)
	}
	if state.Reg.C[0].GetControlData().Save.D[0] == nil {
		t.Fatalf("expected c4 to be saved")
	}

	state = newTestState()
	value := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	if err := state.Stack.PushCell(value); err != nil {
		t.Fatalf("push setcontctr value: %v", err)
	}
	if err := state.Stack.PushContinuation(&testContinuation{name: "cont"}); err != nil {
		t.Fatalf("push setcontctr cont: %v", err)
	}
	if err := SETCONTCTR(4).Interpret(state); err != nil {
		t.Fatalf("setcontctr failed: %v", err)
	}
	gotCont, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop setcontctr cont: %v", err)
	}
	if gotCont.GetControlData().Save.D[0] == nil {
		t.Fatalf("expected c4 to be saved in continuation")
	}
}

func TestCallXAndTryOps(t *testing.T) {
	state := newTestState()
	seenArg := int64(0)
	body := &testContinuation{
		name: "callxargs",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			val, err := s.Stack.PopIntFinite()
			if err != nil {
				return nil, err
			}
			seenArg = val.Int64()
			return nil, nil
		},
	}
	if err := state.Stack.PushInt(big.NewInt(77)); err != nil {
		t.Fatalf("push arg: %v", err)
	}
	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push body: %v", err)
	}
	if err := CALLXARGS(1, 2).Interpret(state); err != nil {
		t.Fatalf("callxargs failed: %v", err)
	}
	if seenArg != 77 {
		t.Fatalf("expected forwarded arg 77, got %d", seenArg)
	}
	if state.Reg.C[0].GetControlData().NumArgs != 2 {
		t.Fatalf("expected return arity 2")
	}

	state = newTestState()
	seenArg = 0
	body = &testContinuation{
		name: "callxargsp",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			val, err := s.Stack.PopIntFinite()
			if err != nil {
				return nil, err
			}
			seenArg = val.Int64()
			return nil, nil
		},
	}
	if err := state.Stack.PushInt(big.NewInt(88)); err != nil {
		t.Fatalf("push arg: %v", err)
	}
	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push body: %v", err)
	}
	if err := CALLXARGSP(1).Interpret(state); err != nil {
		t.Fatalf("callxargsp failed: %v", err)
	}
	if seenArg != 88 || state.Reg.C[0].GetControlData().NumArgs != -1 {
		t.Fatalf("unexpected callxargsp result")
	}

	state = newTestState()
	handled := 0
	cont := &testContinuation{
		name: "try_body",
		onJump: func(*vm.State) (vm.Continuation, error) {
			handled++
			return nil, nil
		},
	}
	handler := &testContinuation{name: "handler"}
	oldC2 := &testContinuation{name: "old_c2"}
	state.Reg.C[2] = oldC2
	if err := state.Stack.PushContinuation(cont); err != nil {
		t.Fatalf("push try body: %v", err)
	}
	if err := state.Stack.PushContinuation(handler); err != nil {
		t.Fatalf("push try handler: %v", err)
	}
	if err := TRY().Interpret(state); err != nil {
		t.Fatalf("try failed: %v", err)
	}
	if handled != 1 {
		t.Fatalf("expected try body to run once, got %d", handled)
	}
	data := state.Reg.C[2].GetControlData()
	if data == nil || data.Save.C[2] != oldC2 || data.Save.C[0] == nil {
		t.Fatalf("expected handler to capture c2 and current continuation")
	}
}

func TestIfBitJmpAndRefOps(t *testing.T) {
	state := newTestState()
	calls := 0
	body := &testContinuation{
		name: "bitjmp",
		onJump: func(*vm.State) (vm.Continuation, error) {
			calls++
			return nil, nil
		},
	}
	if err := state.Stack.PushInt(big.NewInt(0b100)); err != nil {
		t.Fatalf("push bit source: %v", err)
	}
	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push bit body: %v", err)
	}
	op := IFBITJMP(2)
	if err := op.Interpret(state); err != nil {
		t.Fatalf("ifbitjmp failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected IFBITJMP to jump once, got %d", calls)
	}
	back, err := state.Stack.PopIntFinite()
	if err != nil || back.Int64() != 0b100 {
		t.Fatalf("expected source integer to remain on stack: %v err=%v", back, err)
	}

	encoded := op.Serialize().EndCell()
	decoded := IFBITJMP(0)
	if err := decoded.Deserialize(encoded.BeginParse()); err != nil {
		t.Fatalf("deserialize ifbitjmp failed: %v", err)
	}
	if decoded.SerializeText() != "IFBITJMP 2" {
		t.Fatalf("unexpected decoded name: %q", decoded.SerializeText())
	}

	state = newTestState()
	if err := state.Stack.PushInt(big.NewInt(0b001)); err != nil {
		t.Fatalf("push negated source: %v", err)
	}
	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push negated body: %v", err)
	}
	if err := IFNBITJMP(2).Interpret(state); err != nil {
		t.Fatalf("ifnbitjmp failed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected IFNBITJMP to jump once more, got %d", calls)
	}

	refCode := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	state = newTestState()
	state.InitForExecution()
	if err := state.Stack.PushInt(big.NewInt(0b100)); err != nil {
		t.Fatalf("push ref source: %v", err)
	}
	if err := IFBITJMPREF(2, refCode).Interpret(state); err != nil {
		t.Fatalf("ifbitjmpref failed: %v", err)
	}
	if state.CurrentCode.MustLoadUInt(8) != 0xAA {
		t.Fatalf("expected IFBITJMPREF to jump into referenced code")
	}

	refOp := IFNBITJMPREF(1, refCode)
	if err := refOp.Deserialize(refOp.Serialize().EndCell().BeginParse()); err != nil {
		t.Fatalf("deserialize ifnbitjmpref failed: %v", err)
	}
	if refOp.SerializeText() != "IFNBITJMPREF" {
		t.Fatalf("unexpected ref op name after round-trip: %q", refOp.SerializeText())
	}

	runRef := func(t *testing.T, op vm.OP, cond bool, expectedCode uint64) {
		t.Helper()
		state := newTestState()
		state.InitForExecution()
		if err := state.Stack.PushBool(cond); err != nil {
			t.Fatalf("push ref cond: %v", err)
		}
		if err := op.Interpret(state); err != nil {
			t.Fatalf("ref op failed: %v", err)
		}
		if expectedCode != 0 && state.CurrentCode.MustLoadUInt(8) != expectedCode {
			t.Fatalf("unexpected referenced code: %#x", state.CurrentCode.MustLoadUInt(8))
		}
	}

	runRef(t, IFREF(refCode), true, 0xAA)
	runRef(t, IFNOTREF(refCode), false, 0xAA)
	runRef(t, IFJMPREF(refCode), true, 0xAA)
	runRef(t, IFNOTJMPREF(refCode), false, 0xAA)

	runRefNoop := func(t *testing.T, op vm.OP, cond bool) {
		t.Helper()
		state := newTestState()
		state.InitForExecution()
		state.CurrentCode = cell.BeginCell().MustStoreUInt(0x44, 8).EndCell().BeginParse()
		if err := state.Stack.PushBool(cond); err != nil {
			t.Fatalf("push ref noop cond: %v", err)
		}
		if err := op.Interpret(state); err != nil {
			t.Fatalf("ref noop op failed: %v", err)
		}
		if state.CurrentCode.MustLoadUInt(8) != 0x44 {
			t.Fatalf("expected current code to remain unchanged")
		}
	}

	runRefNoop(t, IFREF(refCode), false)
	runRefNoop(t, IFNOTREF(refCode), true)
	runRefNoop(t, IFJMPREF(refCode), false)
	runRefNoop(t, IFNOTJMPREF(refCode), true)

	state = newTestState()
	state.InitForExecution()
	altCalls := 0
	alt := &testContinuation{name: "alt", onJump: func(*vm.State) (vm.Continuation, error) { altCalls++; return nil, nil }}
	if err := state.Stack.PushBool(true); err != nil {
		t.Fatalf("push ifrefelse cond: %v", err)
	}
	if err := state.Stack.PushContinuation(alt); err != nil {
		t.Fatalf("push ifrefelse alt: %v", err)
	}
	if err := IFREFELSE(refCode).Interpret(state); err != nil {
		t.Fatalf("ifrefelse failed: %v", err)
	}
	if altCalls != 0 || state.CurrentCode.MustLoadUInt(8) != 0xAA {
		t.Fatalf("expected IFREFELSE to choose referenced code")
	}

	state = newTestState()
	state.InitForExecution()
	altCalls = 0
	if err := state.Stack.PushBool(false); err != nil {
		t.Fatalf("push ifelseref cond: %v", err)
	}
	if err := state.Stack.PushContinuation(alt); err != nil {
		t.Fatalf("push ifelseref alt: %v", err)
	}
	if err := IFELSEREF(refCode).Interpret(state); err != nil {
		t.Fatalf("ifelseref failed: %v", err)
	}
	if altCalls != 0 || state.CurrentCode.MustLoadUInt(8) != 0xAA {
		t.Fatalf("expected IFELSEREF to choose referenced code")
	}

	state = newTestState()
	state.InitForExecution()
	altCalls = 0
	if err := state.Stack.PushBool(false); err != nil {
		t.Fatalf("push ifrefelse false-branch cond: %v", err)
	}
	if err := state.Stack.PushContinuation(alt); err != nil {
		t.Fatalf("push ifrefelse false-branch alt: %v", err)
	}
	if err := IFREFELSE(refCode).Interpret(state); err != nil {
		t.Fatalf("ifrefelse false branch failed: %v", err)
	}
	if altCalls != 1 {
		t.Fatalf("expected IFREFELSE to call stack continuation, got %d calls", altCalls)
	}

	state = newTestState()
	state.InitForExecution()
	altCalls = 0
	if err := state.Stack.PushBool(true); err != nil {
		t.Fatalf("push ifelseref true-branch cond: %v", err)
	}
	if err := state.Stack.PushContinuation(alt); err != nil {
		t.Fatalf("push ifelseref true-branch alt: %v", err)
	}
	if err := IFELSEREF(refCode).Interpret(state); err != nil {
		t.Fatalf("ifelseref true branch failed: %v", err)
	}
	if altCalls != 1 {
		t.Fatalf("expected IFELSEREF to call stack continuation, got %d calls", altCalls)
	}

	falseCode := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	state = newTestState()
	state.InitForExecution()
	if err := state.Stack.PushBool(false); err != nil {
		t.Fatalf("push ifrefelseref cond: %v", err)
	}
	if err := IFREFELSEREF(refCode, falseCode).Interpret(state); err != nil {
		t.Fatalf("ifrefelseref failed: %v", err)
	}
	if state.CurrentCode.MustLoadUInt(8) != 0xBB {
		t.Fatalf("expected false branch code to be selected")
	}

	state = newTestState()
	state.InitForExecution()
	if err := state.Stack.PushBool(true); err != nil {
		t.Fatalf("push ifrefelseref true cond: %v", err)
	}
	if err := IFREFELSEREF(refCode, falseCode).Interpret(state); err != nil {
		t.Fatalf("ifrefelseref true branch failed: %v", err)
	}
	if state.CurrentCode.MustLoadUInt(8) != 0xAA {
		t.Fatalf("expected true branch code to be selected")
	}

	state = newTestState()
	state.InitForExecution()
	if err := CALLREF(refCode).Interpret(state); err != nil {
		t.Fatalf("callref failed: %v", err)
	}
	if state.CurrentCode.MustLoadUInt(8) != 0xAA {
		t.Fatalf("expected CALLREF to load referenced code")
	}

	state = newTestState()
	state.InitForExecution()
	if err := JMPREF(refCode).Interpret(state); err != nil {
		t.Fatalf("jmpref failed: %v", err)
	}
	if state.CurrentCode.MustLoadUInt(8) != 0xAA {
		t.Fatalf("expected JMPREF to load referenced code")
	}

	state = newTestState()
	state.CurrentCode = cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell().BeginParse()
	state.InitForExecution()
	if err := JMPREFDATA(refCode).Interpret(state); err != nil {
		t.Fatalf("jmprefdata failed: %v", err)
	}
	pushed, err := state.Stack.PopSlice()
	if err != nil || pushed.MustLoadUInt(8) != 0xCC {
		t.Fatalf("expected JMPREFDATA to preserve current code on stack: %v err=%v", pushed, err)
	}
}

func TestArgsVarAndDictJumpOps(t *testing.T) {
	copyCount, more := parseCopyMore(encodeCopyMore(3, -1))
	if copyCount != 3 || more != -1 {
		t.Fatalf("unexpected parse/encode round-trip: copy=%d more=%d", copyCount, more)
	}

	state := newTestState()
	cont := &testContinuation{
		name: "varargs",
		data: &vm.ControlData{NumArgs: vm.ControlDataAllArgs, CP: vm.CP},
	}
	if err := state.Stack.PushInt(big.NewInt(55)); err != nil {
		t.Fatalf("push setcontvarargs arg: %v", err)
	}
	if err := state.Stack.PushContinuation(cont); err != nil {
		t.Fatalf("push setcontvarargs cont: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push setcontvarargs copy count: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push setcontvarargs more: %v", err)
	}
	if err := SETCONTVARARGS().Interpret(state); err != nil {
		t.Fatalf("setcontvarargs failed: %v", err)
	}
	gotCont, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop setcontvarargs cont: %v", err)
	}
	arg, err := gotCont.GetControlData().Stack.PopIntFinite()
	if err != nil || arg.Int64() != 55 || gotCont.GetControlData().NumArgs != 0 {
		t.Fatalf("unexpected setcontvarargs result: arg=%v nargs=%d err=%v", arg, gotCont.GetControlData().NumArgs, err)
	}

	state = newTestState()
	if err := state.Stack.PushContinuation(&testContinuation{name: "setnum"}); err != nil {
		t.Fatalf("push setnum cont: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push setnum more: %v", err)
	}
	if err := SETNUMVARARGS().Interpret(state); err != nil {
		t.Fatalf("setnumvarargs failed: %v", err)
	}
	gotCont, err = state.Stack.PopContinuation()
	if err != nil || gotCont.GetControlData().NumArgs != 2 {
		t.Fatalf("unexpected setnumvarargs result: cont=%v err=%v", gotCont, err)
	}

	state = newTestState()
	state.Reg.C[0] = &testContinuation{
		name: "returnvar",
		data: &vm.ControlData{NumArgs: vm.ControlDataAllArgs, CP: vm.CP},
	}
	if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
		t.Fatalf("push returnvar first: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
		t.Fatalf("push returnvar second: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push returnvar count: %v", err)
	}
	if err := RETURNVARARGS().Interpret(state); err != nil {
		t.Fatalf("returnvarargs failed: %v", err)
	}
	left, err := state.Stack.PopIntFinite()
	if err != nil || left.Int64() != 22 {
		t.Fatalf("unexpected returnvarargs stack result: %v err=%v", left, err)
	}
	saved, err := state.Reg.C[0].GetControlData().Stack.PopIntFinite()
	if err != nil || saved.Int64() != 11 {
		t.Fatalf("unexpected returnvarargs saved value: %v err=%v", saved, err)
	}

	state = newTestState()
	code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse()
	if err := state.Stack.PushSlice(code); err != nil {
		t.Fatalf("push bless code: %v", err)
	}
	if err := BLESS().Interpret(state); err != nil {
		t.Fatalf("bless failed: %v", err)
	}
	gotCont, err = state.Stack.PopContinuation()
	if err != nil || gotCont.GetControlData().NumArgs != vm.ControlDataAllArgs {
		t.Fatalf("unexpected bless continuation: %v err=%v", gotCont, err)
	}

	state = newTestState()
	if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
		t.Fatalf("push blessvar arg: %v", err)
	}
	if err := state.Stack.PushSlice(code); err != nil {
		t.Fatalf("push blessvar code: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push blessvar copy count: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push blessvar more: %v", err)
	}
	if err := BLESSVARARGS().Interpret(state); err != nil {
		t.Fatalf("blessvarargs failed: %v", err)
	}
	gotCont, err = state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop blessvarargs cont: %v", err)
	}
	arg, err = gotCont.GetControlData().Stack.PopIntFinite()
	if err != nil || arg.Int64() != 99 || gotCont.GetControlData().NumArgs != 0 {
		t.Fatalf("unexpected blessvarargs result: arg=%v nargs=%d err=%v", arg, gotCont.GetControlData().NumArgs, err)
	}

	state = newTestState()
	state.Reg.C[1] = &testContinuation{name: "alt"}
	state.Reg.D[0] = cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	if err := state.Stack.PushContinuation(&testContinuation{name: "manyx"}); err != nil {
		t.Fatalf("push setcontctrmanyx cont: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt((1 << 1) | (1 << 4))); err != nil {
		t.Fatalf("push setcontctrmanyx mask: %v", err)
	}
	if err := SETCONTCTRMANYX().Interpret(state); err != nil {
		t.Fatalf("setcontctrmanyx failed: %v", err)
	}
	gotCont, err = state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop setcontctrmanyx cont: %v", err)
	}
	if gotCont.GetControlData().Save.C[1] == nil || gotCont.GetControlData().Save.D[0] == nil {
		t.Fatalf("expected selected control registers to be copied")
	}

	runDictCont := func(t *testing.T, op vm.OP, expectedID int64) {
		t.Helper()
		state := newTestState()
		calls := 0
		state.Reg.C[3] = &testContinuation{
			name: "dict",
			onJump: func(s *vm.State) (vm.Continuation, error) {
				calls++
				id, err := s.Stack.PopIntFinite()
				if err != nil {
					return nil, err
				}
				if id.Int64() != expectedID {
					t.Fatalf("unexpected dict id: %s", id.String())
				}
				return nil, nil
			},
		}
		if err := op.Interpret(state); err != nil {
			t.Fatalf("dict op failed: %v", err)
		}
		if calls != 1 {
			t.Fatalf("expected dict continuation to run once, got %d", calls)
		}
	}

	runDictCont(t, CALLDICT(7), 7)
	runDictCont(t, CALLDICT(0x1FF), 0x1FF)
	runDictCont(t, JMPDICT(12), 12)

	state = newTestState()
	state.Reg.C[3] = &testContinuation{name: "prepared"}
	if err := PREPAREDICT(3).Interpret(state); err != nil {
		t.Fatalf("preparedict failed: %v", err)
	}
	prepared, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop prepared continuation: %v", err)
	}
	preparedID, err := state.Stack.PopIntFinite()
	if err != nil || preparedID.Int64() != 3 || continuationName(t, prepared) != "prepared" {
		t.Fatalf("unexpected prepare dict result: id=%v cont=%v err=%v", preparedID, prepared, err)
	}

	state = newTestState()
	if err := CALLDICT(1).Interpret(state); err == nil {
		t.Fatal("expected CALLDICT without c3 to fail")
	}

	dict := cell.NewDict(8)
	codeCell := cell.BeginCell().MustStoreUInt(0xDD, 8).EndCell()
	if _, err := dict.SetWithMode(cell.BeginCell().MustStoreBigInt(big.NewInt(0x2A), 8).EndCell(), codeCell, cell.DictSetModeSet); err != nil {
		t.Fatalf("seed dictiigetjmpz dict: %v", err)
	}
	state = newTestState()
	state.InitForExecution()
	if err := state.Stack.PushInt(big.NewInt(0x2A)); err != nil {
		t.Fatalf("push dictiigetjmpz key: %v", err)
	}
	if err := state.Stack.PushCell(dict.AsCell()); err != nil {
		t.Fatalf("push dictiigetjmpz root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push dictiigetjmpz bits: %v", err)
	}
	if err := DICTIGETJMPZ().Interpret(state); err != nil {
		t.Fatalf("dictiigetjmpz hit failed: %v", err)
	}
	if state.CurrentCode.MustLoadUInt(8) != 0xDD {
		t.Fatalf("expected dictiigetjmpz to jump into dictionary code")
	}

	state = newTestState()
	if err := state.Stack.PushInt(big.NewInt(0x99)); err != nil {
		t.Fatalf("push dictiigetjmpz miss key: %v", err)
	}
	if err := state.Stack.PushCell(nil); err != nil {
		t.Fatalf("push dictiigetjmpz nil root: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("push dictiigetjmpz miss bits: %v", err)
	}
	if err := DICTIGETJMPZ().Interpret(state); err != nil {
		t.Fatalf("dictiigetjmpz miss failed: %v", err)
	}
	id, err := state.Stack.PopIntFinite()
	if err != nil || id.Int64() != 0x99 {
		t.Fatalf("expected dictiigetjmpz miss to keep key: %v err=%v", id, err)
	}

	roundTrip := []struct {
		name string
		op   vm.OP
		want string
	}{
		{name: "retargs", op: RETARGS(3), want: "RETARGS 3"},
		{name: "blessargs", op: BLESSARGS(2, 1), want: "BLESSARGS 2,1"},
		{name: "callccargs", op: CALLCCARGS(1, 2), want: "CALLCCARGS 1,2"},
	}
	for _, tt := range roundTrip {
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

func TestCallVarargsOps(t *testing.T) {
	state := newTestState()
	seenArg := int64(0)
	body := &testContinuation{
		name: "callxvarargs",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			val, err := s.Stack.PopIntFinite()
			if err != nil {
				return nil, err
			}
			seenArg = val.Int64()
			return nil, nil
		},
	}
	if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
		t.Fatalf("push callxvarargs arg: %v", err)
	}
	if err := state.Stack.PushContinuation(body); err != nil {
		t.Fatalf("push callxvarargs body: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push callxvarargs params: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push callxvarargs retvals: %v", err)
	}
	if err := CALLXVARARGS().Interpret(state); err != nil {
		t.Fatalf("callxvarargs failed: %v", err)
	}
	if seenArg != 99 || state.Reg.C[0].GetControlData().NumArgs != 2 {
		t.Fatalf("unexpected callxvarargs result: arg=%d nargs=%d", seenArg, state.Reg.C[0].GetControlData().NumArgs)
	}

	state = newTestState()
	calls := 0
	state.Reg.C[0] = &testContinuation{
		name: "retvarargs",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			val, err := s.Stack.PopIntFinite()
			if err != nil {
				return nil, err
			}
			if val.Int64() != 77 {
				t.Fatalf("unexpected retvarargs arg: %s", val.String())
			}
			calls++
			return nil, nil
		},
	}
	if err := state.Stack.PushInt(big.NewInt(77)); err != nil {
		t.Fatalf("push retvarargs arg: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push retvarargs count: %v", err)
	}
	if err := RETVARARGS().Interpret(state); err != nil {
		t.Fatalf("retvarargs failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected retvarargs continuation to run once, got %d", calls)
	}
}
