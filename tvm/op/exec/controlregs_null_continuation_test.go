package exec

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func pushNullContinuation(t *testing.T, state *vm.State) {
	t.Helper()
	if err := state.Stack.PushContinuation(nil); err != nil {
		t.Fatalf("push null continuation: %v", err)
	}
}

func TestControlRegistersRejectNullContinuationReference(t *testing.T) {
	t.Run("POPCTR consumes value and keeps register", func(t *testing.T) {
		state := newTestState()
		oldC0 := state.Reg.C[0]
		pushNullContinuation(t, state)

		assertVMErrCode(t, POPCTR(0).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("stack len = %d, want null value consumed", state.Stack.Len())
		}
		if state.Reg.C[0] != oldC0 {
			t.Fatalf("c0 changed to %T", state.Reg.C[0])
		}
	})

	t.Run("SETRETCTR rejects null even for v14 duplicate", func(t *testing.T) {
		state := newTestState()
		state.GlobalVersion = 14
		oldSaved := &testContinuation{name: "already_saved"}
		oldC0 := &testContinuation{
			name: "c0",
			data: &vm.ControlData{Save: vm.Register{
				C: [4]vm.Continuation{oldSaved},
			}},
		}
		state.Reg.C[0] = oldC0
		pushNullContinuation(t, state)

		assertVMErrCode(t, SETRETCTR(0).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("stack len = %d, want null value consumed", state.Stack.Len())
		}
		if state.Reg.C[0] != oldC0 {
			t.Fatal("SETRETCTR changed c0 on failed null save-list write")
		}
	})

	t.Run("SETCONTCTR consumes both operands", func(t *testing.T) {
		state := newTestState()
		pushNullContinuation(t, state)
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target continuation: %v", err)
		}

		assertVMErrCode(t, SETCONTCTR(0).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("stack len = %d, want both operands consumed", state.Stack.Len())
		}
	})

	t.Run("POPSAVE c0 mutates save list before failed set", func(t *testing.T) {
		state := newTestState()
		oldC0 := &testContinuation{name: "old_c0"}
		state.Reg.C[0] = oldC0
		pushNullContinuation(t, state)

		assertVMErrCode(t, POPSAVECTR(0).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("stack len = %d, want null value consumed", state.Stack.Len())
		}
		if state.Reg.C[0] == oldC0 {
			t.Fatal("POPSAVE c0 did not install the return wrapper before failed set")
		}
		saved := state.Reg.C[0].GetControlData().Save.C[0]
		if continuationName(t, saved) != "old_c0" {
			t.Fatalf("saved c0 = %T %v, want old c0", saved, saved)
		}
	})
}

func TestPushEmptyContinuationRegisterPreservesStackType(t *testing.T) {
	for _, test := range []struct {
		name string
		push func(*vm.State) error
	}{
		{
			name: "PUSHCTR",
			push: func(state *vm.State) error {
				return PUSHCTR(2).Interpret(state)
			},
		},
		{
			name: "PUSHCTRX",
			push: func(state *vm.State) error {
				if err := state.Stack.PushSmallInt(2); err != nil {
					return err
				}
				return PUSHCTRX().Interpret(state)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &vm.State{Stack: vm.NewStack()}
			if err := test.push(state); err != nil {
				t.Fatalf("push empty c2: %v", err)
			}

			got, err := state.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop pushed c2: %v", err)
			}
			cont, ok := got.(vm.Continuation)
			if !ok {
				t.Fatalf("pushed empty c2 type = %T, want continuation", got)
			}
			if !vm.IsNullContinuation(cont) {
				t.Fatalf("pushed empty c2 = %T %v, want null continuation reference", cont, cont)
			}
		})
	}
}

func TestPushCtrXEmptyDataRegistersPreserveStackType(t *testing.T) {
	for _, test := range []struct {
		name  string
		index int64
		check func(any) bool
	}{
		{
			name:  "c4 cell",
			index: 4,
			check: func(value any) bool {
				ref, ok := value.(*cell.Cell)
				return ok && ref == nil
			},
		},
		{
			name:  "c5 cell",
			index: 5,
			check: func(value any) bool {
				ref, ok := value.(*cell.Cell)
				return ok && ref == nil
			},
		},
		{
			name:  "c7 tuple",
			index: 7,
			check: func(value any) bool {
				valueTuple, ok := value.(tuple.Tuple)
				return ok && valueTuple.IsNull()
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &vm.State{Stack: vm.NewStack()}
			if err := state.Stack.PushSmallInt(test.index); err != nil {
				t.Fatalf("push register index: %v", err)
			}
			if err := PUSHCTRX().Interpret(state); err != nil {
				t.Fatalf("PUSHCTRX empty register: %v", err)
			}

			got, err := state.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop pushed register: %v", err)
			}
			if !test.check(got) {
				t.Fatalf("pushed empty c%d type/value = %T %#v", test.index, got, got)
			}
		})
	}
}

func TestCurrentCodeDictRejectsTypedNilContinuation(t *testing.T) {
	var c3 *vm.OrdinaryContinuation
	state := &vm.State{Reg: vm.Register{C: [4]vm.Continuation{nil, nil, nil, c3}}}

	_, err := currentCodeDict(state)
	assertVMErrCode(t, err, vmerr.CodeTypeCheck)
}
