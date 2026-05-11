package vm

import (
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestContinuationImplementations(t *testing.T) {
	state := &State{Stack: NewStack()}

	quit := &QuitContinuation{ExitCode: 12}
	if quit.GetControlData() != nil {
		t.Fatal("quit continuation should not have control data")
	}
	if _, err := quit.Jump(state); err == nil {
		t.Fatal("quit continuation should terminate with an error")
	} else {
		assertVMErrorCode(t, err, 12)
	}
	if copiedQuit := quit.Copy().(*QuitContinuation); copiedQuit == quit || copiedQuit.ExitCode != 12 {
		t.Fatal("quit continuation copy should preserve exit code and clone the value")
	}

	handled := HandledException{VMError: vmerr.Error(vmerr.CodeFatal, "fatal")}
	if handled.Error() == "" || !errors.As(handled.Unwrap(), &handled.VMError) {
		t.Fatal("handled exception should expose wrapped vm error")
	}

	if (&ExcQuitContinuation{}).GetControlData() != nil {
		t.Fatal("exception quit continuation should not have control data")
	}
	if _, ok := (&ExcQuitContinuation{}).Copy().(*ExcQuitContinuation); !ok {
		t.Fatal("exception quit continuation should copy as same type")
	}

	ordinary := &OrdinaryContinuation{
		Data: ControlData{
			Save: Register{C: [4]Continuation{nil, &QuitContinuation{ExitCode: 33}}},
		},
		Code: cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse(),
	}
	if ordinary.GetControlData() == nil {
		t.Fatal("ordinary continuation should expose control data")
	}
	state = &State{Stack: NewStack()}
	if _, err := ordinary.Jump(state); err != nil {
		t.Fatalf("ordinary jump: %v", err)
	}
	if state.Reg.C[1] == nil {
		t.Fatal("ordinary continuation should restore saved registers")
	}
	if copiedOrdinary := ordinary.Copy().(*OrdinaryContinuation); copiedOrdinary == ordinary || copiedOrdinary.Data.Stack != nil {
		t.Fatal("ordinary continuation copy should clone the continuation")
	}

	argExt := &ArgExtContinuation{
		Data: ControlData{
			Save: Register{C: [4]Continuation{nil, &QuitContinuation{ExitCode: 44}}},
			CP:   CP,
		},
		Ext: ordinary,
	}
	if argExt.GetControlData() == nil {
		t.Fatal("arg-ext continuation should expose control data")
	}
	next, err := argExt.Jump(&State{})
	if err != nil {
		t.Fatalf("arg-ext jump: %v", err)
	}
	if next != ordinary {
		t.Fatal("arg-ext continuation should return external continuation")
	}
	if _, ok := argExt.Copy().(*ArgExtContinuation); !ok {
		t.Fatal("arg-ext continuation copy should preserve type")
	}

	badArgExt := &ArgExtContinuation{
		Data: ControlData{CP: 1},
		Ext:  ordinary,
	}
	if _, err = badArgExt.Jump(&State{}); err == nil {
		t.Fatal("expected unsupported codepage error")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeInvalidOpcode)
	}

	pushInt := &PushIntContinuation{Int: 55, Next: ordinary}
	state = &State{Stack: NewStack()}
	next, err = pushInt.Jump(state)
	if err != nil {
		t.Fatalf("push int jump: %v", err)
	}
	if next != ordinary || mustPopInt64(t, state.Stack) != 55 {
		t.Fatal("push int continuation should push the value and continue")
	}
	if copiedPush := pushInt.Copy().(*PushIntContinuation); copiedPush.Int != 55 || copiedPush.Next != ordinary {
		t.Fatal("push int continuation copy should preserve fields")
	}
}

func TestLoopContinuations(t *testing.T) {
	simpleBody := &QuitContinuation{ExitCode: 1}
	savedBody := &OrdinaryContinuation{}
	savedBody.Data.Save.C[0] = &QuitContinuation{ExitCode: 2}
	after := &QuitContinuation{ExitCode: 3}
	cond := &QuitContinuation{ExitCode: 4}

	if (&RepeatContinuation{}).GetControlData() != nil {
		t.Fatal("repeat continuation should not have control data")
	}
	if (&AgainContinuation{}).GetControlData() != nil {
		t.Fatal("again continuation should not have control data")
	}
	if (&WhileContinuation{}).GetControlData() != nil {
		t.Fatal("while continuation should not have control data")
	}
	if (&UntilContinuation{}).GetControlData() != nil {
		t.Fatal("until continuation should not have control data")
	}

	state := &State{Stack: NewStack()}
	next, err := (&RepeatContinuation{Count: 0, Body: simpleBody, After: after}).Jump(state)
	if err != nil || next != after {
		t.Fatalf("repeat zero iterations = (%v, %v), want after", next, err)
	}

	state = &State{Stack: NewStack()}
	next, err = (&RepeatContinuation{Count: 2, Body: simpleBody, After: after}).Jump(state)
	if err != nil || next != simpleBody {
		t.Fatalf("repeat next = (%v, %v), want body", next, err)
	}
	if cont, ok := state.Reg.C[0].(*RepeatContinuation); !ok || cont.Count != 1 {
		t.Fatalf("repeat continuation register = %#v", state.Reg.C[0])
	}
	if _, ok := (&RepeatContinuation{Count: 1, Body: simpleBody, After: after}).Copy().(*RepeatContinuation); !ok {
		t.Fatal("repeat continuation copy should preserve type")
	}

	state = &State{Stack: NewStack()}
	next, err = (&RepeatContinuation{Count: 2, Body: savedBody, After: after}).Jump(state)
	if err != nil || next != savedBody || state.Reg.C[0] != nil {
		t.Fatal("repeat continuation should not overwrite c0 for closures with saved c0")
	}

	state = &State{Stack: NewStack()}
	next, err = (&AgainContinuation{Body: simpleBody}).Jump(state)
	if err != nil || next != simpleBody {
		t.Fatalf("again next = (%v, %v), want body", next, err)
	}
	if _, ok := state.Reg.C[0].(*AgainContinuation); !ok {
		t.Fatalf("again continuation register = %#v", state.Reg.C[0])
	}
	if _, ok := (&AgainContinuation{Body: simpleBody}).Copy().(*AgainContinuation); !ok {
		t.Fatal("again continuation copy should preserve type")
	}

	state = &State{Stack: NewStack()}
	next, err = (&AgainContinuation{Body: savedBody}).Jump(state)
	if err != nil || next != savedBody || state.Reg.C[0] != nil {
		t.Fatal("again continuation should keep c0 untouched for closures with saved c0")
	}

	state = &State{Stack: NewStack()}
	if err = state.Stack.PushBool(false); err != nil {
		t.Fatalf("push false: %v", err)
	}
	next, err = (&WhileContinuation{CheckCond: true, Body: simpleBody, Cond: cond, After: after}).Jump(state)
	if err != nil || next != after {
		t.Fatalf("while false branch = (%v, %v), want after", next, err)
	}

	state = &State{Stack: NewStack()}
	if err = state.Stack.PushBool(true); err != nil {
		t.Fatalf("push true: %v", err)
	}
	next, err = (&WhileContinuation{CheckCond: true, Body: simpleBody, Cond: cond, After: after}).Jump(state)
	if err != nil || next != simpleBody {
		t.Fatalf("while true branch = (%v, %v), want body", next, err)
	}
	if cont, ok := state.Reg.C[0].(*WhileContinuation); !ok || cont.CheckCond {
		t.Fatalf("while continuation register = %#v", state.Reg.C[0])
	}

	state = &State{Stack: NewStack()}
	next, err = (&WhileContinuation{CheckCond: false, Body: simpleBody, Cond: cond, After: after}).Jump(state)
	if err != nil || next != cond {
		t.Fatalf("while condition branch = (%v, %v), want cond", next, err)
	}
	if cont, ok := state.Reg.C[0].(*WhileContinuation); !ok || !cont.CheckCond {
		t.Fatalf("while condition register = %#v", state.Reg.C[0])
	}
	if _, ok := (&WhileContinuation{Body: simpleBody, Cond: cond, After: after}).Copy().(*WhileContinuation); !ok {
		t.Fatal("while continuation copy should preserve type")
	}

	state = &State{Stack: NewStack()}
	if err = state.Stack.PushBool(true); err != nil {
		t.Fatalf("push until true: %v", err)
	}
	next, err = (&UntilContinuation{Body: simpleBody, After: after}).Jump(state)
	if err != nil || next != after {
		t.Fatalf("until true branch = (%v, %v), want after", next, err)
	}

	state = &State{Stack: NewStack()}
	if err = state.Stack.PushBool(false); err != nil {
		t.Fatalf("push until false: %v", err)
	}
	until := &UntilContinuation{Body: simpleBody, After: after}
	next, err = until.Jump(state)
	if err != nil || next != simpleBody {
		t.Fatalf("until false branch = (%v, %v), want body", next, err)
	}
	if state.Reg.C[0] != until {
		t.Fatal("until continuation should store itself in c0")
	}
	if _, ok := until.Copy().(*UntilContinuation); !ok {
		t.Fatal("until continuation copy should preserve type")
	}
}

func TestExecHelpers(t *testing.T) {
	state := &State{
		CurrentCode: cell.BeginCell().MustStoreUInt(0x11, 8).EndCell().BeginParse(),
		Stack:       NewStack(),
	}
	state.Reg.C[0] = &QuitContinuation{ExitCode: 9}
	if err := state.Return(); err == nil {
		t.Fatal("return should jump to the previous continuation")
	} else {
		assertVMErrorCode(t, err, 9)
	}
	if state.Reg.C[0].(*QuitContinuation).ExitCode != 0 {
		t.Fatalf("return should replace c0 with exit 0, got %d", state.Reg.C[0].(*QuitContinuation).ExitCode)
	}

	state.Reg.C[1] = &QuitContinuation{ExitCode: 10}
	if err := state.ReturnAlt(); err == nil {
		t.Fatal("return alt should jump to the previous alternative continuation")
	} else {
		assertVMErrorCode(t, err, 10)
	}
	if state.Reg.C[1].(*QuitContinuation).ExitCode != 1 {
		t.Fatalf("return alt should replace c1 with exit 1, got %d", state.Reg.C[1].(*QuitContinuation).ExitCode)
	}

	if err := state.returnTo(0, 0, 1, 2); err == nil {
		t.Fatal("expected too many return arguments to fail")
	}

	callState := &State{
		CurrentCode: cell.BeginCell().MustStoreUInt(0x12, 8).EndCell().BeginParse(),
		Stack:       NewStack(),
	}
	target := &OrdinaryContinuation{
		Code: cell.BeginCell().MustStoreUInt(0x34, 8).EndCell().BeginParse(),
	}
	if err := callState.Call(target); err != nil {
		t.Fatalf("simple call: %v", err)
	}
	if callState.Reg.C[0] == nil {
		t.Fatal("call should install return continuation")
	}
	op, err := callState.CurrentCode.Copy().LoadUInt(8)
	if err != nil {
		t.Fatalf("read current code: %v", err)
	}
	if op != 0x34 {
		t.Fatalf("current opcode = %x, want 34", op)
	}

	complexCallState := &State{
		CurrentCode: cell.BeginCell().MustStoreUInt(0x13, 8).EndCell().BeginParse(),
		Stack:       NewStack(),
	}
	pushInts(t, complexCallState.Stack, 1, 2)
	if err := complexCallState.Call(&OrdinaryContinuation{
		Data: ControlData{NumArgs: 1, CP: CP},
		Code: cell.BeginCell().MustStoreUInt(0x35, 8).EndCell().BeginParse(),
	}); err != nil {
		t.Fatalf("complex call: %v", err)
	}
	if complexCallState.Stack.Len() != 1 || mustPopInt64(t, complexCallState.Stack) != 2 {
		t.Fatal("complex call should keep only requested arguments on the active stack")
	}

	jumpState := &State{
		CurrentCode: cell.BeginCell().EndCell().BeginParse(),
		Stack:       NewStack(),
	}
	jumpTarget := &OrdinaryContinuation{
		Data: ControlData{
			Save: Register{C: [4]Continuation{&QuitContinuation{ExitCode: 77}}},
		},
		Code: cell.BeginCell().MustStoreUInt(0x56, 8).EndCell().BeginParse(),
	}
	if err := jumpState.Jump(jumpTarget); err != nil {
		t.Fatalf("simple jump: %v", err)
	}
	if jumpState.Reg.C[0] == nil {
		t.Fatal("jump should restore saved c0")
	}

	stackPlanState := &State{Stack: NewStack()}
	pushInts(t, stackPlanState.Stack, 1)
	if _, err := planContinuationStack(stackPlanState.Stack, &ControlData{NumArgs: 2}, 1, continuationActionCall); err == nil {
		t.Fatal("expected closure call stack underflow")
	}
	if _, err := planContinuationStack(stackPlanState.Stack, nil, 2, continuationActionJump); err == nil {
		t.Fatal("expected jump stack underflow")
	}

	extractState := &State{
		CurrentCode: cell.BeginCell().MustStoreUInt(0x78, 8).EndCell().BeginParse(),
		Stack:       NewStack(),
	}
	pushInts(t, extractState.Stack, 1, 2, 3)
	extractState.Reg.C[0] = &QuitContinuation{ExitCode: 1}
	extractState.Reg.C[1] = &QuitContinuation{ExitCode: 2}
	extractState.Reg.C[2] = &QuitContinuation{ExitCode: 3}

	cont, err := extractState.ExtractCurrentContinuation(3, 2, 5)
	if err != nil {
		t.Fatalf("extract current continuation: %v", err)
	}
	if cont.Data.NumArgs != 5 || cont.Data.CP != 0 {
		t.Fatalf("extracted control data = %#v", cont.Data)
	}
	assertPopInts(t, extractState.Stack, 3, 2)
	assertPopInts(t, cont.Data.Stack, 1)
	if cont.Data.Save.C[0] == nil || cont.Data.Save.C[1] == nil {
		t.Fatal("extract current continuation should save requested control registers")
	}
	if extractState.Reg.C[0].(*QuitContinuation).ExitCode != 0 || extractState.Reg.C[1].(*QuitContinuation).ExitCode != 1 {
		t.Fatal("extract current continuation should replace saved registers with quit continuations")
	}

	extractAllState := &State{
		CurrentCode: cell.BeginCell().MustStoreUInt(0x79, 8).EndCell().BeginParse(),
		Stack:       NewStack(),
	}
	pushInts(t, extractAllState.Stack, 4, 5)
	allCont, err := extractAllState.ExtractCurrentContinuation(0, -1, 0)
	if err != nil {
		t.Fatalf("extract all stack continuation: %v", err)
	}
	if extractAllState.Stack.Len() != 2 || allCont.Data.Stack.Len() != 0 {
		t.Fatal("extract all stack branch should swap the active stack and captured stack")
	}
}
