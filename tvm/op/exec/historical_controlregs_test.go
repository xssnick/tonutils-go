package exec

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestHistoricalPopC3Cell(t *testing.T) {
	state := newTestState()
	state.GlobalVersion = 0
	state.Historical.PopC3Cell = true
	state.CP = 7
	state.Cells.Init(state)
	defer state.Cells.FinishExecution()

	code := cell.BeginCell().MustStoreUInt(0x76, 8).EndCell()
	for i, wantGas := range []int64{100, 125} {
		if err := state.Stack.PushCell(code); err != nil {
			t.Fatal(err)
		}
		if err := POPCTR(3).Interpret(state); err != nil {
			t.Fatalf("POP c3 load %d: %v", i, err)
		}

		cont, ok := state.Reg.C[3].(*vm.OrdinaryContinuation)
		if !ok {
			t.Fatalf("c3 = %T, want ordinary continuation", state.Reg.C[3])
		}
		if !bytes.Equal(cont.Code.RawCell().Hash(), code.Hash()) || cont.Code.BitsLeft() != code.BitsSize() {
			t.Fatal("c3 code differs from the input cell")
		}
		if cont.Data.CP != 7 || cont.Data.NumArgs != vm.ControlDataAllArgs {
			t.Fatalf("continuation CP/NumArgs = %d/%d", cont.Data.CP, cont.Data.NumArgs)
		}
		if state.Stack.Len() != 0 {
			t.Fatalf("stack depth = %d, want 0", state.Stack.Len())
		}
		if got := state.Gas.Used(); got != wantGas {
			t.Fatalf("gas after load %d = %d, want %d", i, got, wantGas)
		}
		if state.Steps != 0 {
			t.Fatalf("auto-BLESS added %d VM steps", state.Steps)
		}
	}
}

func TestHistoricalPopC3CellScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int
		enabled bool
		index   int
	}{
		{name: "default version zero", version: 0, index: 3},
		{name: "version one", version: 1, enabled: true, index: 3},
		{name: "latest version", version: vm.MaxSupportedGlobalVersion, enabled: true, index: 3},
		{name: "c0", enabled: true, index: 0},
		{name: "c1", enabled: true, index: 1},
		{name: "c2", enabled: true, index: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newTestState()
			state.GlobalVersion = tc.version
			state.Historical.PopC3Cell = tc.enabled
			old := &vm.QuitContinuation{ExitCode: 1}
			state.Reg.C[tc.index] = old
			state.Cells.Init(state)
			defer state.Cells.FinishExecution()

			if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
				t.Fatal(err)
			}
			assertVMErrCode(t, POPCTR(tc.index).Interpret(state), vmerr.CodeTypeCheck)
			if state.Reg.C[tc.index] != old || state.Gas.Used() != 0 || state.Stack.Len() != 0 {
				t.Fatal("rejected POP must consume its argument without changing the register or loading the cell")
			}
		})
	}

	for _, name := range []string{"POPCTRX", "setter"} {
		t.Run(name, func(t *testing.T) {
			state := newTestState()
			state.GlobalVersion = 0
			state.Historical.PopC3Cell = true
			code := cell.BeginCell().EndCell()
			if name == "setter" {
				assertVMErrCode(t, setControlRegister(state, 3, code), vmerr.CodeTypeCheck)
				return
			}

			if err := state.Stack.PushCell(code); err != nil {
				t.Fatal(err)
			}
			if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
				t.Fatal(err)
			}
			assertVMErrCode(t, POPCTRX().Interpret(state), vmerr.CodeTypeCheck)
		})
	}
}

func TestHistoricalPopC3Continuation(t *testing.T) {
	state := newTestState()
	state.GlobalVersion = 0
	state.Historical.PopC3Cell = true
	cont := &vm.QuitContinuation{ExitCode: 1}
	if err := state.Stack.PushOwnedContinuation(cont); err != nil {
		t.Fatal(err)
	}
	if err := POPCTR(3).Interpret(state); err != nil {
		t.Fatal(err)
	}
	if state.Reg.C[3] != cont || state.Gas.Used() != 0 {
		t.Fatal("POP c3 changed a continuation or charged cell load gas")
	}
}

func TestHistoricalPopC3CellLoadErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    *cell.Cell
		wantErr int64
		wantGas int64
	}{
		{name: "missing library", code: unresolvedLibraryCell(t), wantErr: vmerr.CodeCellUnderflow, wantGas: 100},
		{name: "null cell", wantErr: vmerr.CodeTypeCheck},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newTestState()
			state.GlobalVersion = 0
			state.Historical.PopC3Cell = true
			old := &vm.QuitContinuation{ExitCode: 1}
			state.Reg.C[3] = old
			state.Cells.Init(state)
			defer state.Cells.FinishExecution()

			if err := state.Stack.PushCell(tc.code); err != nil {
				t.Fatal(err)
			}
			assertVMErrCode(t, POPCTR(3).Interpret(state), tc.wantErr)
			if state.Reg.C[3] != old || state.Stack.Len() != 0 {
				t.Fatal("failed load must consume the cell without changing c3")
			}
			if got := state.Gas.Used(); got != tc.wantGas {
				t.Fatalf("gas = %d, want %d", got, tc.wantGas)
			}
		})
	}

	t.Run("gas checked after version zero instruction", func(t *testing.T) {
		state := newTestState()
		state.GlobalVersion = 0
		state.Historical.PopC3Cell = true
		state.Gas = vm.GasWithLimit(99)
		state.Cells.Init(state)
		defer state.Cells.FinishExecution()

		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatal(err)
		}
		if err := POPCTR(3).Interpret(state); err != nil {
			t.Fatalf("version zero must defer the gas check: %v", err)
		}
		assertVMErrCode(t, state.CheckGas(), vmerr.CodeOutOfGas)
		if got := state.Gas.Used(); got != 100 {
			t.Fatalf("gas = %d, want 100", got)
		}
	})
}
