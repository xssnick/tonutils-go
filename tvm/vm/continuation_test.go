package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestExcQuitContinuationReturnsOriginalCode(t *testing.T) {
	state := &State{Stack: NewStack()}
	if err := state.Stack.PushInt(big.NewInt(55)); err != nil {
		t.Fatalf("push exception code: %v", err)
	}

	_, err := (&ExcQuitContinuation{}).Jump(state)
	if err == nil {
		t.Fatal("expected exception exit")
	}

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) {
		t.Fatalf("expected VMError, got %T", err)
	}
	var handled HandledException
	if !errors.As(err, &handled) {
		t.Fatalf("expected handled exception wrapper, got %T", err)
	}
	if vmErr.Code != 55 {
		t.Fatalf("expected exit code 55, got %d", vmErr.Code)
	}
}

func TestOrdinaryJumpReentryStartsFromSamePosition(t *testing.T) {
	state := &State{Stack: NewStack(), Gas: NewGas()}
	state.InitForExecution()

	code := cell.BeginCell().MustStoreUInt(0xDEAD, 16).EndCell().MustBeginParse()
	cont := &OrdinaryContinuation{Data: ControlData{NumArgs: ControlDataAllArgs}, Code: code}

	if _, err := cont.Jump(state); err != nil {
		t.Fatalf("first jump: %v", err)
	}
	scratch := state.CurrentCode
	if v := state.CurrentCode.MustLoadUInt(16); v != 0xDEAD {
		t.Fatalf("unexpected code after first jump: %x", v)
	}

	if _, err := cont.Jump(state); err != nil {
		t.Fatalf("second jump: %v", err)
	}
	if state.CurrentCode != scratch {
		t.Fatal("expected the state-owned scratch slice to be reused in place")
	}
	if v := state.CurrentCode.MustLoadUInt(16); v != 0xDEAD {
		t.Fatalf("re-entry does not start from the continuation start: %x", v)
	}

	if code.BitsLeft() != 16 {
		t.Fatalf("continuation code was advanced in place, bits left: %d", code.BitsLeft())
	}
}

func TestOrdinaryJumpKeepsCapturedReturnCodePristine(t *testing.T) {
	state := &State{Stack: NewStack(), Gas: NewGas()}
	state.InitForExecution()

	main := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().MustBeginParse()
	if _, err := (&OrdinaryContinuation{Data: ControlData{NumArgs: ControlDataAllArgs}, Code: main}).Jump(state); err != nil {
		t.Fatalf("entry jump: %v", err)
	}

	// Call captures the raw CurrentCode pointer into the return continuation,
	// so the jumps below must not overwrite it in place.
	body := cell.BeginCell().MustStoreUInt(0xCDEF, 16).EndCell().MustBeginParse()
	if err := state.Call(&OrdinaryContinuation{Data: ControlData{NumArgs: ControlDataAllArgs}, Code: body}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if v := state.CurrentCode.MustLoadUInt(8); v != 0xCD {
		t.Fatalf("unexpected body code: %x", v)
	}

	other := cell.BeginCell().MustStoreUInt(0x11, 8).EndCell().MustBeginParse()
	if _, err := (&OrdinaryContinuation{Data: ControlData{NumArgs: ControlDataAllArgs}, Code: other}).Jump(state); err != nil {
		t.Fatalf("intermediate jump: %v", err)
	}

	if err := state.Return(); err != nil {
		t.Fatalf("return: %v", err)
	}
	if v := state.CurrentCode.MustLoadUInt(8); v != 0xAB {
		t.Fatalf("captured return code was clobbered, got: %x", v)
	}
}
