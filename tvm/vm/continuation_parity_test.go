package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestRegisterDefineRejectsNullC7Tuple(t *testing.T) {
	var reg Register

	if reg.Define(7, tuple.Tuple{}) {
		t.Fatal("null tuple must not define c7")
	}

	empty := tuple.NewTupleValue()
	if !reg.Define(7, empty) {
		t.Fatal("explicit empty tuple should define c7")
	}
	if reg.C7.IsNull() || reg.C7.Len() != 0 {
		t.Fatal("c7 should store a defined empty tuple")
	}
}

func TestOrdinaryContinuationJumpRestoresCP0AndEmptySavedC7(t *testing.T) {
	state := NewExecutionState(DefaultGlobalVersion, NewGas(), nil, tuple.NewTupleValue("old"), NewStack())
	state.PrepareExecution(cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse())
	state.CP = 7

	cont := &OrdinaryContinuation{
		Data: ControlData{
			Save: Register{
				C7: tuple.NewTupleValue(),
			},
			CP: 0,
		},
		Code: cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell().BeginParse(),
	}

	if _, err := cont.Jump(state); err != nil {
		t.Fatalf("ordinary jump failed: %v", err)
	}
	if state.CP != 0 {
		t.Fatalf("state cp = %d, want 0", state.CP)
	}
	if state.Reg.C7.IsNull() || state.Reg.C7.Len() != 0 {
		t.Fatal("saved empty c7 should be restored")
	}

	op, err := state.CurrentCode.Copy().LoadUInt(8)
	if err != nil {
		t.Fatalf("load restored code: %v", err)
	}
	if op != 0xBB {
		t.Fatalf("restored opcode = %x, want bb", op)
	}
}

func TestArgExtContinuationJumpAllowsCP0(t *testing.T) {
	state := &State{Stack: NewStack(), CP: 9}
	next := &QuitContinuation{ExitCode: 17}
	cont := &ArgExtContinuation{
		Data: ControlData{CP: 0},
		Ext:  next,
	}

	got, err := cont.Jump(state)
	if err != nil {
		t.Fatalf("arg-ext jump failed: %v", err)
	}
	if got != next {
		t.Fatalf("next continuation = %T, want %T", got, next)
	}
	if state.CP != 0 {
		t.Fatalf("state cp = %d, want 0", state.CP)
	}
}
