package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestRegisterRejectsNullContinuationReference(t *testing.T) {
	var typedNil *QuitContinuation

	for _, test := range []struct {
		name string
		cont Continuation
	}{
		{name: "stack sentinel", cont: nullContinuationValue},
		{name: "typed nil", cont: typedNil},
	} {
		t.Run(test.name, func(t *testing.T) {
			reg := Register{}
			if reg.Set(0, test.cont) {
				t.Fatal("Register.Set accepted a null continuation reference")
			}
			if reg.C[0] != nil {
				t.Fatalf("Register.Set changed c0 to %T", reg.C[0])
			}
			if reg.Define(0, test.cont) {
				t.Fatal("Register.Define accepted a null continuation reference")
			}
			if reg.C[0] != nil {
				t.Fatalf("Register.Define changed c0 to %T", reg.C[0])
			}
		})
	}
}

func TestRegisterTreatsNullContinuationReferenceAsEmptySlot(t *testing.T) {
	valid := &QuitContinuation{ExitCode: 17}
	reg := Register{C: [4]Continuation{nullContinuationValue}}
	if !reg.Define(0, valid) {
		t.Fatal("Register.Define treated a null continuation reference as a filled slot")
	}
	if reg.C[0] != valid {
		t.Fatalf("c0 = %T %v, want replacement continuation", reg.C[0], reg.C[0])
	}

	dst := Register{C: [4]Continuation{valid}}
	dst.AdjustWith(&Register{C: [4]Continuation{nullContinuationValue}})
	if dst.C[0] != valid {
		t.Fatal("Register.AdjustWith overwrote c0 with a null continuation reference")
	}

	reg = Register{C: [4]Continuation{nullContinuationValue}}
	copy := reg.Copy()
	if copy.C[0] != nil {
		t.Fatalf("Register.Copy retained null continuation as %T", copy.C[0])
	}
}

func TestExtractCurrentContinuationMovesSavedC2(t *testing.T) {
	c2 := &QuitContinuation{ExitCode: 23}
	state := &State{
		CurrentCode: cell.BeginCell().EndCell().MustBeginParse(),
		Stack:       NewStack(),
		Reg:         Register{C: [4]Continuation{nil, nil, c2}},
	}

	cont, err := state.ExtractCurrentContinuation(1<<2, -1, ControlDataAllArgs)
	if err != nil {
		t.Fatalf("extract continuation: %v", err)
	}
	if cont.Data.Save.C[2] != c2 {
		t.Fatalf("saved c2 = %T %v, want original continuation", cont.Data.Save.C[2], cont.Data.Save.C[2])
	}
	if state.Reg.C[2] != nil {
		t.Fatalf("active c2 = %T %v, want empty after move", state.Reg.C[2], state.Reg.C[2])
	}
}

func TestRegisterGetPreservesEmptyContinuationType(t *testing.T) {
	var registers Register

	got := registers.Get(2)
	cont, ok := got.(Continuation)
	if !ok {
		t.Fatalf("empty c2 type = %T, want continuation", got)
	}
	if !IsNullContinuation(cont) {
		t.Fatalf("empty c2 = %T %v, want null continuation reference", cont, cont)
	}
}

func TestComplexContinuationPreclearsRegistersBeforeStackGasError(t *testing.T) {
	newDeepStack := func(t *testing.T) *Stack {
		t.Helper()
		stack := NewStack()
		for i := 0; i <= FreeStackDepth; i++ {
			if err := stack.PushSmallInt(int64(i)); err != nil {
				t.Fatalf("push captured stack value: %v", err)
			}
		}
		return stack
	}
	newState := func() *State {
		return &State{
			GlobalVersion: 4,
			CP:            0,
			CurrentCode:   cell.BeginCell().EndCell().MustBeginParse(),
			Gas:           GasWithLimit(0),
			Stack:         NewStack(),
			Reg: Register{
				C: [4]Continuation{
					&QuitContinuation{ExitCode: 10},
					&QuitContinuation{ExitCode: 11},
					&QuitContinuation{ExitCode: 12},
					&QuitContinuation{ExitCode: 13},
				},
				D:  [2]*cell.Cell{cell.BeginCell().EndCell(), cell.BeginCell().EndCell()},
				C7: tuple.NewTupleValue(),
			},
		}
	}
	newTarget := func(save Register) *OrdinaryContinuation {
		return &OrdinaryContinuation{
			Data: ControlData{
				Save:    save,
				Stack:   newDeepStack(t),
				NumArgs: ControlDataAllArgs,
				CP:      0,
			},
			Code: cell.BeginCell().EndCell().MustBeginParse(),
		}
	}
	assertEmpty := func(t *testing.T, state *State, indexes ...int) {
		t.Helper()
		for _, i := range indexes {
			if !IsNullContinuation(state.Reg.C[i]) {
				t.Fatalf("c%d = %T, want precleared", i, state.Reg.C[i])
			}
		}
		for i, data := range state.Reg.D {
			if data != nil {
				t.Fatalf("c%d was not precleared", i+4)
			}
		}
		if !state.Reg.C7.IsNull() {
			t.Fatal("c7 was not precleared")
		}
	}

	t.Run("jump", func(t *testing.T) {
		state := newState()
		save := Register{
			C: [4]Continuation{
				&QuitContinuation{},
				&QuitContinuation{},
				&QuitContinuation{},
				&QuitContinuation{},
			},
			D:  [2]*cell.Cell{cell.BeginCell().EndCell(), cell.BeginCell().EndCell()},
			C7: tuple.NewTupleValue(),
		}

		err := state.JumpArgs(newTarget(save), -1)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeOutOfGas {
			t.Fatalf("jump error = %v, want out-of-gas", err)
		}
		assertEmpty(t, state, 0, 1, 2, 3)
	})

	t.Run("call moves c0 and preclears saved registers", func(t *testing.T) {
		state := newState()
		save := Register{
			C: [4]Continuation{
				nil,
				&QuitContinuation{},
				&QuitContinuation{},
				&QuitContinuation{},
			},
			D:  [2]*cell.Cell{cell.BeginCell().EndCell(), cell.BeginCell().EndCell()},
			C7: tuple.NewTupleValue(),
		}

		err := state.CallArgs(newTarget(save), -1, -1)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeOutOfGas {
			t.Fatalf("call error = %v, want out-of-gas", err)
		}
		assertEmpty(t, state, 0, 1, 2, 3)
	})

	t.Run("call without control data keeps c0 before stack gas error", func(t *testing.T) {
		state := newState()
		state.Stack = newDeepStack(t)
		oldC0 := state.Reg.C[0]

		err := state.CallArgs(&QuitContinuation{ExitCode: 9}, state.Stack.Len(), -1)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeOutOfGas {
			t.Fatalf("call error = %v, want out-of-gas", err)
		}
		if state.Reg.C[0] != oldC0 {
			t.Fatalf("c0 = %T, want original continuation", state.Reg.C[0])
		}
	})
}

func TestNullSavedC0DoesNotChangeContinuationControlFlow(t *testing.T) {
	t.Run("call remains a call", func(t *testing.T) {
		oldC0 := &QuitContinuation{ExitCode: 31}
		state := &State{
			CP:          0,
			CurrentCode: cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().MustBeginParse(),
			Stack:       NewStack(),
			Reg:         Register{C: [4]Continuation{oldC0}},
		}
		target := &OrdinaryContinuation{
			Data: ControlData{
				Save:    Register{C: [4]Continuation{nullContinuationValue}},
				NumArgs: ControlDataAllArgs,
				CP:      0,
			},
			Code: cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell().MustBeginParse(),
		}

		if err := state.Call(target); err != nil {
			t.Fatalf("call target: %v", err)
		}
		ret, ok := state.Reg.C[0].(*OrdinaryContinuation)
		if !ok {
			t.Fatalf("c0 = %T, want generated return continuation", state.Reg.C[0])
		}
		if ret.Data.Save.C[0] != oldC0 {
			t.Fatalf("return continuation saved c0 = %T %v, want old c0", ret.Data.Save.C[0], ret.Data.Save.C[0])
		}
	})

	t.Run("repeat installs loop return", func(t *testing.T) {
		body := &OrdinaryContinuation{
			Data: ControlData{
				Save: Register{C: [4]Continuation{nullContinuationValue}},
				CP:   0,
			},
			Code: cell.BeginCell().EndCell().MustBeginParse(),
		}
		after := &QuitContinuation{ExitCode: 0}
		loop := &RepeatContinuation{Count: 2, Body: body, After: after}
		state := &State{Stack: NewStack()}

		next, err := loop.Jump(state)
		if err != nil {
			t.Fatalf("jump repeat continuation: %v", err)
		}
		if next != body {
			t.Fatalf("next = %T, want body", next)
		}
		repeat, ok := state.Reg.C[0].(*RepeatContinuation)
		if !ok || repeat.Count != 1 {
			t.Fatalf("c0 = %#v, want repeat continuation with one iteration", state.Reg.C[0])
		}
	})
}
