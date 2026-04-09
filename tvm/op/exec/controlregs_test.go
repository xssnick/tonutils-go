package exec

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestPOPCTRPushCTRForC4(t *testing.T) {
	state := newTestState()
	val := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()

	if err := state.Stack.PushCell(val); err != nil {
		t.Fatalf("push val: %v", err)
	}
	if err := POPCTR(4).Interpret(state); err != nil {
		t.Fatalf("popctr failed: %v", err)
	}
	if state.Reg.D[0] == nil || string(state.Reg.D[0].Hash()) != string(val.Hash()) {
		t.Fatalf("expected c4 to store pushed cell, got %#v", state.Reg.D[0])
	}

	if err := PUSHCTR(4).Interpret(state); err != nil {
		t.Fatalf("pushctr failed: %v", err)
	}
	got, err := state.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop pushed c4: %v", err)
	}
	if string(got.Hash()) != string(val.Hash()) {
		t.Fatalf("unexpected pushed c4 value")
	}
}

func TestPOPCTRRejectsNonContinuationForC0(t *testing.T) {
	state := newTestState()
	val := cell.BeginCell().EndCell()

	if err := state.Stack.PushCell(val); err != nil {
		t.Fatalf("push val: %v", err)
	}

	err := POPCTR(0).Interpret(state)
	if err == nil {
		t.Fatal("expected type check")
	}

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeTypeCheck {
		t.Fatalf("expected type check, got %v", err)
	}
}

func TestSETRETCTRStoresValueInC0Save(t *testing.T) {
	state := newTestState()
	state.Reg.C[0] = &testContinuation{name: "c0"}
	val := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	if err := state.Stack.PushCell(val); err != nil {
		t.Fatalf("push val: %v", err)
	}
	if err := SETRETCTR(4).Interpret(state); err != nil {
		t.Fatalf("setretctr failed: %v", err)
	}

	save := vm.ForceControlData(state.Reg.C[0]).GetControlData().Save
	if save.D[0] == nil || string(save.D[0].Hash()) != string(val.Hash()) {
		t.Fatalf("expected c0 save[c4] to contain pushed cell")
	}
}

func TestPOPSAVECTRSavesPreviousValue(t *testing.T) {
	state := newTestState()
	state.Reg.C[0] = &testContinuation{name: "c0"}
	oldVal := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	newVal := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	state.Reg.D[0] = oldVal

	if err := state.Stack.PushCell(newVal); err != nil {
		t.Fatalf("push new val: %v", err)
	}
	if err := POPSAVECTR(4).Interpret(state); err != nil {
		t.Fatalf("popsavectr failed: %v", err)
	}

	if state.Reg.D[0] == nil || string(state.Reg.D[0].Hash()) != string(newVal.Hash()) {
		t.Fatalf("expected c4 to be replaced with new value")
	}

	save := vm.ForceControlData(state.Reg.C[0]).GetControlData().Save
	if save.D[0] == nil || string(save.D[0].Hash()) != string(oldVal.Hash()) {
		t.Fatalf("expected c0 save[c4] to contain previous value")
	}
}

func TestPOPCTRXRejectsInvalidFixedGapIndex(t *testing.T) {
	state := newTestState()
	val := cell.BeginCell().EndCell()

	if err := state.Stack.PushCell(val); err != nil {
		t.Fatalf("push val: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(6)); err != nil {
		t.Fatalf("push idx: %v", err)
	}

	err := POPCTRX().Interpret(state)
	if err == nil {
		t.Fatal("expected range check")
	}

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeRangeCheck {
		t.Fatalf("expected range check, got %v", err)
	}
}

func TestControlRegisterAdvancedOpRoundTrips(t *testing.T) {
	cases := []struct {
		name string
		op   vm.OP
		want string
	}{
		{name: "pushctr", op: PUSHCTR(4), want: "c4 PUSH"},
		{name: "popctr", op: POPCTR(5), want: "c5 POP"},
		{name: "setretctr", op: SETRETCTR(4), want: "c4 SETRETCTR"},
		{name: "setaltctr", op: SETALTCTR(5), want: "c5 SETALTCTR"},
		{name: "popsavectr", op: POPSAVECTR(4), want: "c4 POPSAVE"},
		{name: "savealtctr", op: SAVEALTCTR(7), want: "c7 SAVEALTCTR"},
		{name: "savebothctr", op: SAVEBOTHCTR(4), want: "c4 SAVEBOTHCTR"},
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

func TestPOPSAVECTRForC0AndPOPCTRXSuccess(t *testing.T) {
	state := newTestState()
	oldC0 := &testContinuation{name: "old_c0"}
	newC0 := &testContinuation{name: "new_c0"}
	state.Reg.C[0] = oldC0

	if err := state.Stack.PushContinuation(newC0); err != nil {
		t.Fatalf("push replacement c0: %v", err)
	}
	if err := POPSAVECTR(0).Interpret(state); err != nil {
		t.Fatalf("popsavectr c0 failed: %v", err)
	}
	if continuationName(t, state.Reg.C[0]) != "new_c0" {
		t.Fatalf("expected c0 to be replaced, got %#v", state.Reg.C[0])
	}

	state = newTestState()
	val := cell.BeginCell().MustStoreUInt(0xAC, 8).EndCell()
	if err := state.Stack.PushCell(val); err != nil {
		t.Fatalf("push c4 value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push c4 index: %v", err)
	}
	if err := POPCTRX().Interpret(state); err != nil {
		t.Fatalf("popctrx success failed: %v", err)
	}
	if state.Reg.D[0] == nil || string(state.Reg.D[0].Hash()) != string(val.Hash()) {
		t.Fatalf("expected c4 to be updated through POPCTRX")
	}
}
