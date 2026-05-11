package exec

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestSETCONTARGSCopiesArgsIntoClosure(t *testing.T) {
	state := newTestState()
	cont := &testContinuation{
		name: "body",
		data: &vm.ControlData{NumArgs: vm.ControlDataAllArgs, CP: vm.CP},
	}

	if err := state.Stack.PushInt(big.NewInt(55)); err != nil {
		t.Fatalf("push arg: %v", err)
	}
	if err := state.Stack.PushContinuation(cont); err != nil {
		t.Fatalf("push cont: %v", err)
	}

	if err := SETCONTARGS(1, 0).Interpret(state); err != nil {
		t.Fatalf("setcontargs failed: %v", err)
	}

	gotCont, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop cont: %v", err)
	}
	data := gotCont.GetControlData()
	if data == nil || data.Stack == nil {
		t.Fatal("expected captured stack on continuation")
	}
	if data.NumArgs != 0 {
		t.Fatalf("expected nargs to be 0, got %d", data.NumArgs)
	}
	got, err := data.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop captured arg: %v", err)
	}
	if got.Int64() != 55 {
		t.Fatalf("expected captured arg 55, got %s", got.String())
	}
	if state.Stack.Len() != 0 {
		t.Fatalf("expected source stack to be drained, got %d", state.Stack.Len())
	}
}

func TestSETCONTARGSRejectsTooManyCopiedArgs(t *testing.T) {
	state := newTestState()
	cont := &testContinuation{
		name: "body",
		data: &vm.ControlData{NumArgs: 0, CP: vm.CP},
	}

	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push arg: %v", err)
	}
	if err := state.Stack.PushContinuation(cont); err != nil {
		t.Fatalf("push cont: %v", err)
	}

	err := SETCONTARGS(1, -1).Interpret(state)
	if err == nil {
		t.Fatal("expected stack overflow")
	}

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeStackOverflow {
		t.Fatalf("expected stack overflow, got %v", err)
	}
}

func TestRETURNARGSMovesResidualIntoC0Closure(t *testing.T) {
	state := newTestState()
	state.Reg.C[0] = &testContinuation{
		name: "ret",
		data: &vm.ControlData{NumArgs: vm.ControlDataAllArgs, CP: vm.CP},
	}

	if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
		t.Fatalf("push first: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
		t.Fatalf("push second: %v", err)
	}

	if err := RETURNARGS(1).Interpret(state); err != nil {
		t.Fatalf("returnargs failed: %v", err)
	}

	if state.Stack.Len() != 1 {
		t.Fatalf("expected one return value on stack, got %d", state.Stack.Len())
	}
	got, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop return value: %v", err)
	}
	if got.Int64() != 22 {
		t.Fatalf("expected 22 on stack, got %s", got.String())
	}

	data := state.Reg.C[0].GetControlData()
	if data == nil || data.Stack == nil {
		t.Fatal("expected c0 closure stack to be populated")
	}
	saved, err := data.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop saved residual: %v", err)
	}
	if saved.Int64() != 11 {
		t.Fatalf("expected saved residual 11, got %s", saved.String())
	}
}

func TestBLESSARGSCapturesCodeAndStack(t *testing.T) {
	state := newTestState()
	state.CP = 7
	code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse()

	if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
		t.Fatalf("push arg: %v", err)
	}
	if err := state.Stack.PushSlice(code); err != nil {
		t.Fatalf("push code: %v", err)
	}

	if err := BLESSARGS(1, 0).Interpret(state); err != nil {
		t.Fatalf("blessargs failed: %v", err)
	}

	cont, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop continuation: %v", err)
	}
	ord, ok := cont.(*vm.OrdinaryContinuation)
	if !ok {
		t.Fatalf("expected ordinary continuation, got %T", cont)
	}
	if ord.Data.CP != 7 {
		t.Fatalf("expected cp 7, got %d", ord.Data.CP)
	}
	got, err := ord.Data.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop captured stack value: %v", err)
	}
	if got.Int64() != 99 {
		t.Fatalf("expected captured value 99, got %s", got.String())
	}
	if ord.Code.MustLoadUInt(8) != 0xAA {
		t.Fatalf("unexpected blessed code")
	}
}

func TestSetContAndBlessArgsEncodingMatchesContOps(t *testing.T) {
	cases := []struct {
		name string
		got  uint64
		want uint64
	}{
		{
			name: "setcontargs_copy1_more_minus1",
			got:  SETCONTARGS(1, -1).Serialize().EndCell().BeginParse().MustLoadUInt(16),
			want: 0xEC1F,
		},
		{
			name: "setcontargs_copy2_more0",
			got:  SETCONTARGS(2, 0).Serialize().EndCell().BeginParse().MustLoadUInt(16),
			want: 0xEC20,
		},
		{
			name: "blessargs_copy3_more5",
			got:  BLESSARGS(3, 5).Serialize().EndCell().BeginParse().MustLoadUInt(16),
			want: 0xEE35,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("unexpected encoding: got 0x%X want 0x%X", tt.got, tt.want)
			}
		})
	}
}

func TestSETCONTCTRMANYVersionAndMaskChecks(t *testing.T) {
	var (
		err   error
		vmErr vmerr.VMError
	)

	state := newTestState()
	state.GlobalVersion = 8
	if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
		t.Fatalf("push continuation: %v", err)
	}

	if err := SETCONTCTRMANY(1).Interpret(state); err != nil {
		t.Fatalf("setcontctrmany should be available before version 9, got %v", err)
	}

	state = newTestState()
	state.GlobalVersion = 9
	if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
		t.Fatalf("push continuation: %v", err)
	}
	err = SETCONTCTRMANY(1 << 6).Interpret(state)
	if err == nil {
		t.Fatal("expected range check for c6 mask")
	}
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeRangeCheck {
		t.Fatalf("expected range check, got %v", err)
	}

}

func TestSETCONTCTRMANYCopiesRegisters(t *testing.T) {
	state := newTestState()
	state.GlobalVersion = 9
	state.Reg.C[1] = &testContinuation{name: "alt"}
	state.Reg.D[0] = cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	target := &testContinuation{name: "target"}

	if err := state.Stack.PushContinuation(target); err != nil {
		t.Fatalf("push target: %v", err)
	}

	if err := SETCONTCTRMANY((1 << 1) | (1 << 4)).Interpret(state); err != nil {
		t.Fatalf("setcontctrmany failed: %v", err)
	}

	cont, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop continuation: %v", err)
	}
	data := cont.GetControlData()
	if data == nil {
		t.Fatal("expected control data")
	}
	if continuationName(t, data.Save.C[1]) != "alt" {
		t.Fatalf("expected copied c1, got %#v", data.Save.C[1])
	}
	if data.Save.D[0] == nil || string(data.Save.D[0].Hash()) != string(state.Reg.D[0].Hash()) {
		t.Fatalf("expected copied c4")
	}
}
