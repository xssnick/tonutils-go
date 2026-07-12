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

func TestTVM14SetContCtrXDuplicateSaveListWrite(t *testing.T) {
	oldCont := &testContinuation{name: "old"}
	newCont := &testContinuation{name: "new"}

	makeTarget := func() vm.Continuation {
		return &testContinuation{
			name: "target",
			data: &vm.ControlData{
				Save: vm.Register{
					C: [4]vm.Continuation{oldCont},
				},
			},
		}
	}

	t.Run("v13 duplicate write is type check", func(t *testing.T) {
		state := newTestState()
		state.GlobalVersion = 13
		if err := state.Stack.PushContinuation(newCont); err != nil {
			t.Fatalf("push new continuation: %v", err)
		}
		if err := state.Stack.PushContinuation(makeTarget()); err != nil {
			t.Fatalf("push target continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push index: %v", err)
		}
		assertVMErrCode(t, SETCONTCTRX().Interpret(state), vmerr.CodeTypeCheck)
	})

	t.Run("v14 duplicate write is ignored", func(t *testing.T) {
		state := newTestState()
		state.GlobalVersion = 14
		if err := state.Stack.PushContinuation(newCont); err != nil {
			t.Fatalf("push new continuation: %v", err)
		}
		if err := state.Stack.PushContinuation(makeTarget()); err != nil {
			t.Fatalf("push target continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push index: %v", err)
		}
		if err := SETCONTCTRX().Interpret(state); err != nil {
			t.Fatalf("SETCONTCTRX v14 failed: %v", err)
		}
		got, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target continuation: %v", err)
		}
		if continuationName(t, got.GetControlData().Save.C[0]) != "old" {
			t.Fatalf("duplicate write should leave old saved c0 intact, got %v", got.GetControlData().Save.C[0])
		}
	})

	t.Run("v14 wrong type is still type check", func(t *testing.T) {
		state := newTestState()
		state.GlobalVersion = 14
		if err := state.Stack.PushInt(big.NewInt(42)); err != nil {
			t.Fatalf("push wrong value: %v", err)
		}
		if err := state.Stack.PushContinuation(makeTarget()); err != nil {
			t.Fatalf("push target continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push index: %v", err)
		}
		assertVMErrCode(t, SETCONTCTRX().Interpret(state), vmerr.CodeTypeCheck)
	})
}

func TestTVM14ControlRegisterDuplicateSaveListWrites(t *testing.T) {
	oldCont := &testContinuation{name: "old"}
	newCont := &testContinuation{name: "new"}
	makeTarget := func() vm.Continuation {
		return &testContinuation{
			name: "target",
			data: &vm.ControlData{
				Save: vm.Register{
					C: [4]vm.Continuation{oldCont},
				},
			},
		}
	}

	cases := []struct {
		name    string
		prepare func(*testing.T, *vm.State)
		run     func(*vm.State) (*vm.Register, error)
	}{
		{
			name: "SETCONTCTR",
			prepare: func(t *testing.T, state *vm.State) {
				t.Helper()
				if err := state.Stack.PushContinuation(newCont); err != nil {
					t.Fatalf("push new continuation: %v", err)
				}
				if err := state.Stack.PushContinuation(makeTarget()); err != nil {
					t.Fatalf("push target continuation: %v", err)
				}
			},
			run: func(state *vm.State) (*vm.Register, error) {
				if err := SETCONTCTR(0).Interpret(state); err != nil {
					return nil, err
				}
				cont, err := state.Stack.PopContinuation()
				if err != nil {
					return nil, err
				}
				return &cont.GetControlData().Save, nil
			},
		},
		{
			name: "SETRETCTR",
			prepare: func(t *testing.T, state *vm.State) {
				t.Helper()
				state.Reg.C[0] = makeTarget()
				if err := state.Stack.PushContinuation(newCont); err != nil {
					t.Fatalf("push new continuation: %v", err)
				}
			},
			run: func(state *vm.State) (*vm.Register, error) {
				if err := SETRETCTR(0).Interpret(state); err != nil {
					return nil, err
				}
				return &state.Reg.C[0].GetControlData().Save, nil
			},
		},
		{
			name: "SETALTCTR",
			prepare: func(t *testing.T, state *vm.State) {
				t.Helper()
				state.Reg.C[1] = makeTarget()
				if err := state.Stack.PushContinuation(newCont); err != nil {
					t.Fatalf("push new continuation: %v", err)
				}
			},
			run: func(state *vm.State) (*vm.Register, error) {
				if err := SETALTCTR(0).Interpret(state); err != nil {
					return nil, err
				}
				return &state.Reg.C[1].GetControlData().Save, nil
			},
		},
		{
			name: "SAVECTR",
			prepare: func(t *testing.T, state *vm.State) {
				t.Helper()
				state.Reg.C[0] = makeTarget()
			},
			run: func(state *vm.State) (*vm.Register, error) {
				if err := SAVECTR(0).Interpret(state); err != nil {
					return nil, err
				}
				return &state.Reg.C[0].GetControlData().Save, nil
			},
		},
		{
			name: "SAVEALTCTR",
			prepare: func(t *testing.T, state *vm.State) {
				t.Helper()
				state.Reg.C[0] = newCont
				state.Reg.C[1] = makeTarget()
			},
			run: func(state *vm.State) (*vm.Register, error) {
				if err := SAVEALTCTR(0).Interpret(state); err != nil {
					return nil, err
				}
				return &state.Reg.C[1].GetControlData().Save, nil
			},
		},
		{
			name: "SETCONTCTRMANY",
			prepare: func(t *testing.T, state *vm.State) {
				t.Helper()
				state.Reg.C[0] = newCont
				if err := state.Stack.PushContinuation(makeTarget()); err != nil {
					t.Fatalf("push target continuation: %v", err)
				}
			},
			run: func(state *vm.State) (*vm.Register, error) {
				if err := SETCONTCTRMANY(1).Interpret(state); err != nil {
					return nil, err
				}
				cont, err := state.Stack.PopContinuation()
				if err != nil {
					return nil, err
				}
				return &cont.GetControlData().Save, nil
			},
		},
		{
			name: "SETCONTCTRMANYX",
			prepare: func(t *testing.T, state *vm.State) {
				t.Helper()
				state.Reg.C[0] = newCont
				if err := state.Stack.PushContinuation(makeTarget()); err != nil {
					t.Fatalf("push target continuation: %v", err)
				}
				if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
					t.Fatalf("push mask: %v", err)
				}
			},
			run: func(state *vm.State) (*vm.Register, error) {
				if err := SETCONTCTRMANYX().Interpret(state); err != nil {
					return nil, err
				}
				cont, err := state.Stack.PopContinuation()
				if err != nil {
					return nil, err
				}
				return &cont.GetControlData().Save, nil
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name+"/v13", func(t *testing.T) {
			state := newTestState()
			state.GlobalVersion = 13
			tt.prepare(t, state)
			save, err := tt.run(state)
			if err == nil {
				t.Fatalf("expected duplicate write type check, got save %#v", save)
			}
			assertVMErrCode(t, err, vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"/v14", func(t *testing.T) {
			state := newTestState()
			state.GlobalVersion = 14
			tt.prepare(t, state)
			save, err := tt.run(state)
			if err != nil {
				t.Fatalf("duplicate write should be ignored in v14: %v", err)
			}
			if continuationName(t, save.C[0]) != "old" {
				t.Fatalf("duplicate write should leave old saved c0 intact, got %v", save.C[0])
			}
		})
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
		{name: "savectr", op: SAVECTR(4), want: "c4 SAVECTR"},
		{name: "popsavectr", op: POPSAVECTR(4), want: "c4 POPSAVE"},
		{name: "savealtctr", op: SAVEALTCTR(7), want: "c7 SAVEALTCTR"},
		{name: "savebothctr", op: SAVEBOTHCTR(4), want: "c4 SAVEBOTHCTR"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			encoded := tt.op.Serialize().EndCell()
			decoded := tt.op
			if err := decoded.Deserialize(encoded.MustBeginParse()); err != nil {
				t.Fatalf("deserialize failed: %v", err)
			}
			if decoded.SerializeText() != tt.want {
				t.Fatalf("unexpected round-trip name: %q", decoded.SerializeText())
			}
		})
	}

	short := PUSHCTR(0)
	if err := short.Deserialize(cell.BeginCell().MustStoreSlice(pushCtrBitPrefix.Data, pushCtrBitPrefix.Bits).EndCell().MustBeginParse()); err == nil {
		t.Fatal("expected short control register suffix decode error")
	}
}

func TestSetContCtrAdditionalErrorStackEffects(t *testing.T) {
	t.Run("ShortStackPreservesValue", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push value: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, SETCONTCTR(4).Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("SETCONTCTR short-stack path mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("BadContinuationConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		val := cell.BeginCell().MustStoreUInt(0xC4, 8).EndCell()
		if err := state.Stack.PushCell(val); err != nil {
			t.Fatalf("push value: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad continuation: %v", err)
		}

		assertVMErrCode(t, SETCONTCTR(4).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bad continuation to consume top only, stack len=%d", state.Stack.Len())
		}
		got, err := state.Stack.PopCell()
		if err != nil {
			t.Fatalf("pop value: %v", err)
		}
		if string(got.Hash()) != string(val.Hash()) {
			t.Fatal("unexpected remaining value")
		}
	})

	t.Run("BadC0SavedValueConsumesInputs", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad c0 value: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target continuation: %v", err)
		}

		assertVMErrCode(t, SETCONTCTR(0).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected bad c0 saved value to consume inputs, stack len=%d", state.Stack.Len())
		}
	})
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

func TestControlRegisterAdditionalErrorStackEffects(t *testing.T) {
	t.Run("FixedOpsEmptyStack", func(t *testing.T) {
		cases := []struct {
			name string
			op   vm.OP
		}{
			{name: "POPCTR", op: POPCTR(4)},
			{name: "SETRETCTR", op: SETRETCTR(4)},
			{name: "SETALTCTR", op: SETALTCTR(4)},
			{name: "POPSAVECTR", op: POPSAVECTR(4)},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				state := newTestState()
				assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeStackUnderflow)
				if state.Stack.Len() != 0 {
					t.Fatalf("%s empty-stack path mutated stack, len=%d", tc.name, state.Stack.Len())
				}
			})
		}
	})

	t.Run("PopSaveC0BadValueConsumesValue", func(t *testing.T) {
		state := newTestState()
		oldC0 := &testContinuation{name: "old_c0"}
		state.Reg.C[0] = oldC0
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad c0 value: %v", err)
		}

		assertVMErrCode(t, POPSAVECTR(0).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected bad POPSAVECTR c0 value to be consumed, stack len=%d", state.Stack.Len())
		}
		if state.Reg.C[0] != oldC0 {
			t.Fatal("POPSAVECTR c0 typecheck should not replace c0")
		}
	})

	t.Run("PopSaveC4BadValueConsumesValue", func(t *testing.T) {
		state := newTestState()
		oldC0 := &testContinuation{name: "old_c0"}
		state.Reg.C[0] = oldC0
		oldC4 := cell.BeginCell().MustStoreUInt(0xC4, 8).EndCell()
		state.Reg.D[0] = oldC4
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad c4 value: %v", err)
		}

		assertVMErrCode(t, POPSAVECTR(4).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected bad POPSAVECTR c4 value to be consumed, stack len=%d", state.Stack.Len())
		}
		if state.Reg.C[0] != oldC0 {
			t.Fatal("POPSAVECTR c4 typecheck should not replace c0")
		}
		if state.Reg.D[0] == nil || string(state.Reg.D[0].Hash()) != string(oldC4.Hash()) {
			t.Fatal("POPSAVECTR c4 typecheck should not replace c4")
		}
	})

	t.Run("PushCtrXBadIndexTypeConsumesIndex", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad index: %v", err)
		}

		assertVMErrCode(t, PUSHCTRX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected PUSHCTRX bad index to be consumed, stack len=%d", state.Stack.Len())
		}
	})

	t.Run("PopCtrXShortStackPreservesValue", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
			t.Fatalf("push lone index: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, POPCTRX().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("POPCTRX short-stack path mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("PopCtrXBadIndexTypeConsumesIndexOnly", func(t *testing.T) {
		state := newTestState()
		val := cell.BeginCell().MustStoreUInt(0xC4, 8).EndCell()
		if err := state.Stack.PushCell(val); err != nil {
			t.Fatalf("push c4 value: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad index: %v", err)
		}

		assertVMErrCode(t, POPCTRX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected POPCTRX bad index to consume index only, stack len=%d", state.Stack.Len())
		}
		got, err := state.Stack.PopCell()
		if err != nil {
			t.Fatalf("pop c4 value: %v", err)
		}
		if string(got.Hash()) != string(val.Hash()) {
			t.Fatal("unexpected remaining c4 value")
		}
	})

	t.Run("PopCtrXBadC4ValueConsumesInputs", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad c4 value: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
			t.Fatalf("push c4 index: %v", err)
		}

		assertVMErrCode(t, POPCTRX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected POPCTRX bad c4 value to consume inputs, stack len=%d", state.Stack.Len())
		}
	})

	t.Run("SetContCtrXBadIndexTypeConsumesIndexOnly", func(t *testing.T) {
		state := newTestState()
		val := cell.BeginCell().MustStoreUInt(0xC4, 8).EndCell()
		if err := state.Stack.PushCell(val); err != nil {
			t.Fatalf("push saved value: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target continuation: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad index: %v", err)
		}

		assertVMErrCode(t, SETCONTCTRX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected SETCONTCTRX bad index to consume index only, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target continuation: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected remaining target continuation")
		}
		got, err := state.Stack.PopCell()
		if err != nil {
			t.Fatalf("pop saved value: %v", err)
		}
		if string(got.Hash()) != string(val.Hash()) {
			t.Fatal("unexpected remaining saved value")
		}
	})

	t.Run("SetContCtrXBadContinuationConsumesIndexAndTopOnly", func(t *testing.T) {
		state := newTestState()
		val := cell.BeginCell().MustStoreUInt(0xC4, 8).EndCell()
		if err := state.Stack.PushCell(val); err != nil {
			t.Fatalf("push saved value: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
			t.Fatalf("push c4 index: %v", err)
		}

		assertVMErrCode(t, SETCONTCTRX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected SETCONTCTRX bad continuation to leave value only, stack len=%d", state.Stack.Len())
		}
		got, err := state.Stack.PopCell()
		if err != nil {
			t.Fatalf("pop saved value: %v", err)
		}
		if string(got.Hash()) != string(val.Hash()) {
			t.Fatal("unexpected remaining saved value")
		}
	})

	t.Run("SetContCtrXBadC4ValueConsumesInputs", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad c4 value: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
			t.Fatalf("push c4 index: %v", err)
		}

		assertVMErrCode(t, SETCONTCTRX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected SETCONTCTRX bad c4 value to consume inputs, stack len=%d", state.Stack.Len())
		}
	})
}
