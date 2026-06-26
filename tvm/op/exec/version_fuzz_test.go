package exec

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

var fuzzExecControlRegisterIndexes = [...]int{0, 1, 2, 3, 4, 5, 7}

func fuzzExecVersion(raw int64) int {
	version := int(raw % int64(vm.DefaultGlobalVersion+1))
	if version < 0 {
		version = -version
	}
	return version
}

func TestFuzzExecVersionCoversDefaultRange(t *testing.T) {
	for version := 0; version <= vm.DefaultGlobalVersion; version++ {
		if got := fuzzExecVersion(int64(version)); got != version {
			t.Fatalf("seed version %d mapped to %d", version, got)
		}
	}
	if got := fuzzExecVersion(-int64(vm.DefaultGlobalVersion)); got != vm.DefaultGlobalVersion {
		t.Fatalf("negative default version mapped to %d, want %d", got, vm.DefaultGlobalVersion)
	}
	for _, raw := range []int64{
		-1,
		-int64(vm.DefaultGlobalVersion) - 1,
		-int64(vm.DefaultGlobalVersion) - 2,
		-123456789,
		123456789,
	} {
		got := fuzzExecVersion(raw)
		if got < 0 || got > vm.DefaultGlobalVersion {
			t.Fatalf("raw version %d mapped to %d, want within [0, %d]", raw, got, vm.DefaultGlobalVersion)
		}
	}
}

func FuzzTVMVersionedControlRegisterDuplicateSaveWrites(f *testing.F) {
	for version := int64(0); version <= int64(vm.DefaultGlobalVersion); version++ {
		for opKind := uint8(0); opKind < 9; opKind++ {
			for rawIndex := range fuzzExecControlRegisterIndexes {
				f.Add(version, opKind, uint8(rawIndex), false)
				f.Add(version, opKind, uint8(rawIndex), true)
			}
		}
	}
	f.Add(int64(14), uint8(1), uint8(0), true)
	f.Add(int64(14), uint8(1), uint8(1), true)

	f.Fuzz(func(t *testing.T, rawVersion int64, rawOp, rawIndex uint8, wrongType bool) {
		version := fuzzExecVersion(rawVersion)
		opKind := rawOp % 9
		idx := fuzzExecControlRegisterIndex(rawIndex)

		oldCont := &testContinuation{name: "old"}
		newCont := &testContinuation{name: "new"}
		oldCell := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		newCell := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		oldTuple := tuple.NewTupleValue(big.NewInt(0xAA))
		newTuple := tuple.NewTupleValue(big.NewInt(0xBB), big.NewInt(0xCC))
		state := newTestState()
		state.GlobalVersion = version
		state.GlobalVersionConfigured = true

		makeTarget := func() vm.Continuation {
			save := vm.Register{}
			setExecControlRegisterValue(t, &save, idx, oldCont, oldCell, oldTuple)
			return &testContinuation{
				name: "target",
				data: &vm.ControlData{Save: save},
			}
		}
		setSource := func() {
			setExecControlRegisterValue(t, &state.Reg, idx, newCont, newCell, newTuple)
		}
		pushValue := func(correctType bool) {
			switch {
			case idx < 4 && correctType:
				pushExecContinuation(t, state, newCont)
			case idx >= 4 && idx < 6 && correctType:
				pushExecCell(t, state, newCell)
			case idx == 7 && correctType:
				pushExecTuple(t, state, newTuple)
			case idx >= 4 && idx < 6:
				pushExecContinuation(t, state, newCont)
			default:
				pushExecCell(t, state, newCell)
			}
		}
		wrongType = wrongType && (opKind == 1 || opKind == 2 || opKind == 4 || opKind == 7)

		var err error
		var save *vm.Register
		switch opKind {
		case 0:
			state.Reg.C[0] = makeTarget()
			if idx != 0 {
				setSource()
			}
			err = SAVECTR(idx).Interpret(state)
			if err == nil {
				save = &state.Reg.C[0].GetControlData().Save
			}
		case 1:
			state.Reg.C[0] = makeTarget()
			pushValue(!wrongType)
			err = SETRETCTR(idx).Interpret(state)
			if err == nil {
				save = &state.Reg.C[0].GetControlData().Save
			}
		case 2:
			state.Reg.C[1] = makeTarget()
			pushValue(!wrongType)
			err = SETALTCTR(idx).Interpret(state)
			if err == nil {
				save = &state.Reg.C[1].GetControlData().Save
			}
		case 3:
			state.Reg.C[1] = makeTarget()
			if idx != 1 {
				setSource()
			}
			err = SAVEALTCTR(idx).Interpret(state)
			if err == nil {
				save = &state.Reg.C[1].GetControlData().Save
			}
		case 4:
			pushValue(!wrongType)
			pushExecContinuation(t, state, makeTarget())
			err = SETCONTCTR(idx).Interpret(state)
			if err == nil {
				cont := popExecContinuation(t, state)
				save = &cont.GetControlData().Save
			}
		case 5:
			setSource()
			pushExecContinuation(t, state, makeTarget())
			err = SETCONTCTRMANY(1 << idx).Interpret(state)
			if err == nil {
				cont := popExecContinuation(t, state)
				save = &cont.GetControlData().Save
			}
		case 6:
			setSource()
			pushExecContinuation(t, state, makeTarget())
			pushExecInt(t, state, int64(1<<idx))
			err = SETCONTCTRMANYX().Interpret(state)
			if err == nil {
				cont := popExecContinuation(t, state)
				save = &cont.GetControlData().Save
			}
		case 7:
			pushValue(!wrongType)
			pushExecContinuation(t, state, makeTarget())
			pushExecInt(t, state, int64(idx))
			err = SETCONTCTRX().Interpret(state)
			if err == nil {
				cont := popExecContinuation(t, state)
				save = &cont.GetControlData().Save
			}
		default:
			state.Reg.C[0] = makeTarget()
			state.Reg.C[1] = makeTarget()
			if idx >= 2 {
				setSource()
			}
			err = SAVEBOTHCTR(idx).Interpret(state)
			if err != nil {
				t.Fatalf("SAVEBOTHCTR duplicate save-list write should be ignored in v%d: %v", version, err)
			}
			assertExecDuplicateSavePreserved(t, &state.Reg.C[0].GetControlData().Save, idx, oldCont, oldCell, oldTuple)
			assertExecDuplicateSavePreserved(t, &state.Reg.C[1].GetControlData().Save, idx, oldCont, oldCell, oldTuple)
			return
		}

		if !wrongType && (version >= 14 || idx == 7) {
			if err != nil {
				t.Fatalf("duplicate save-list write should be ignored in v%d: %v", version, err)
			}
			assertExecDuplicateSavePreserved(t, save, idx, oldCont, oldCell, oldTuple)
			return
		}

		assertVMErrCode(t, err, vmerr.CodeTypeCheck)
	})
}

func fuzzExecControlRegisterIndex(raw uint8) int {
	return fuzzExecControlRegisterIndexes[int(raw)%len(fuzzExecControlRegisterIndexes)]
}

func setExecControlRegisterValue(t *testing.T, r *vm.Register, idx int, cont vm.Continuation, c *cell.Cell, tup tuple.Tuple) {
	t.Helper()

	switch {
	case idx < 4:
		r.C[idx] = cont
	case idx < 6:
		r.D[idx-4] = c
	case idx == 7:
		r.C7 = tup
	default:
		t.Fatalf("unsupported control register index %d", idx)
	}
}

func assertExecDuplicateSavePreserved(t *testing.T, save *vm.Register, idx int, oldCont vm.Continuation, oldCell *cell.Cell, oldTuple tuple.Tuple) {
	t.Helper()

	switch {
	case idx < 4:
		if continuationName(t, save.C[idx]) != continuationName(t, oldCont) {
			t.Fatalf("duplicate save-list write replaced saved c%d", idx)
		}
	case idx < 6:
		if save.D[idx-4] == nil || string(save.D[idx-4].Hash()) != string(oldCell.Hash()) {
			t.Fatalf("duplicate save-list write replaced saved c%d", idx)
		}
	case idx == 7:
		if save.C7.Len() != oldTuple.Len() {
			t.Fatalf("duplicate save-list write replaced saved c7")
		}
	default:
		t.Fatalf("unsupported control register index %d", idx)
	}
}

func pushExecContinuation(t *testing.T, state *vm.State, cont vm.Continuation) {
	t.Helper()
	if err := state.Stack.PushContinuation(cont); err != nil {
		t.Fatalf("push continuation: %v", err)
	}
}

func popExecContinuation(t *testing.T, state *vm.State) vm.Continuation {
	t.Helper()
	cont, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop continuation: %v", err)
	}
	return cont
}

func pushExecInt(t *testing.T, state *vm.State, v int64) {
	t.Helper()
	if err := state.Stack.PushInt(big.NewInt(v)); err != nil {
		t.Fatalf("push int: %v", err)
	}
}

func pushExecCell(t *testing.T, state *vm.State, v *cell.Cell) {
	t.Helper()
	if err := state.Stack.PushCell(v); err != nil {
		t.Fatalf("push cell: %v", err)
	}
}

func pushExecTuple(t *testing.T, state *vm.State, v tuple.Tuple) {
	t.Helper()
	if err := state.Stack.PushTuple(v); err != nil {
		t.Fatalf("push tuple: %v", err)
	}
}
