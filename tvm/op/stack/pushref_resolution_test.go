package stack

import (
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestPushRefSliceDefersReferencedCellLoadUntilInterpret(t *testing.T) {
	payload := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := pushRefTestCode(0x89, payload)
	state := pushRefTestState(t, code)
	op := PUSHSLICE(cell.BeginCell().EndCell().MustBeginParse())

	if err := op.Deserialize(state.CurrentCode); err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}
	if got := state.Gas.Used(); got != 0 {
		t.Fatalf("Deserialize consumed referenced-cell gas: got=%d want=0", got)
	}
	if state.Cells.IsCellLoaded(payload) {
		t.Fatal("Deserialize marked referenced cell as loaded")
	}

	if err := op.Interpret(state); err != nil {
		t.Fatalf("Interpret failed: %v", err)
	}
	if got := state.Gas.Used(); got != vm.CellLoadGasPrice {
		t.Fatalf("Interpret gas = %d, want %d", got, vm.CellLoadGasPrice)
	}
	got, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	assertPushRefSliceCell(t, got, payload)
}

func TestPushRefSpecialCellResolution(t *testing.T) {
	sliceTarget := cell.BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	contTarget := cell.BeginCell().MustStoreUInt(0x72, 8).EndCell()
	libraries := pushRefTestLibraries(t, sliceTarget, contTarget)

	tests := []struct {
		name      string
		opcode    uint64
		reference *cell.Cell
		libraries []*cell.Cell
		want      *cell.Cell
		wantErr   int64
	}{
		{
			name:      "PUSHREFSLICE resolves library",
			opcode:    0x89,
			reference: pushRefTestLibraryCell(t, sliceTarget.Hash()),
			libraries: []*cell.Cell{libraries},
			want:      sliceTarget,
		},
		{
			name:      "PUSHREFCONT resolves library",
			opcode:    0x8A,
			reference: pushRefTestLibraryCell(t, contTarget.Hash()),
			libraries: []*cell.Cell{libraries},
			want:      contTarget,
		},
		{
			name:      "PUSHREFSLICE rejects missing library",
			opcode:    0x89,
			reference: pushRefTestLibraryCell(t, sliceTarget.Hash()),
			wantErr:   vmerr.CodeCellUnderflow,
		},
		{
			name:      "PUSHREFCONT rejects missing library",
			opcode:    0x8A,
			reference: pushRefTestLibraryCell(t, contTarget.Hash()),
			wantErr:   vmerr.CodeCellUnderflow,
		},
		{
			name:      "PUSHREFSLICE rejects pruned cell",
			opcode:    0x89,
			reference: pushRefTestPrunedCell(t),
			wantErr:   vmerr.CodeCellUnderflow,
		},
		{
			name:      "PUSHREFCONT rejects pruned cell",
			opcode:    0x8A,
			reference: pushRefTestPrunedCell(t),
			wantErr:   vmerr.CodeCellUnderflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := pushRefTestCode(tt.opcode, tt.reference)
			state := pushRefTestState(t, code, tt.libraries...)

			var op vm.OP
			if tt.opcode == 0x89 {
				op = PUSHSLICE(cell.BeginCell().EndCell().MustBeginParse())
			} else {
				op = PUSHREFCONT(cell.BeginCell().EndCell())
			}
			if err := op.Deserialize(state.CurrentCode); err != nil {
				t.Fatalf("Deserialize failed: %v", err)
			}

			err := op.Interpret(state)
			if tt.wantErr != 0 {
				assertPushRefErrorCode(t, err, tt.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("Interpret failed: %v", err)
			}

			if tt.opcode == 0x89 {
				got, err := state.Stack.PopSlice()
				if err != nil {
					t.Fatalf("PopSlice failed: %v", err)
				}
				assertPushRefSliceCell(t, got, tt.want)
				return
			}

			got, err := state.Stack.PopContinuation()
			if err != nil {
				t.Fatalf("PopContinuation failed: %v", err)
			}
			ordinary, ok := got.(*vm.OrdinaryContinuation)
			if !ok {
				t.Fatalf("continuation type = %T, want *vm.OrdinaryContinuation", got)
			}
			assertPushRefSliceCell(t, ordinary.Code, tt.want)
		})
	}
}

func pushRefTestState(t *testing.T, code *cell.Cell, libraries ...*cell.Cell) *vm.State {
	t.Helper()

	state := vm.NewExecutionState(13, vm.GasWithLimit(10_000), cell.BeginCell().EndCell(), tuple.Tuple{}, vm.NewStack(), libraries...)
	codeSlice, err := code.BeginParse()
	if err != nil {
		t.Fatalf("failed to parse code: %v", err)
	}
	state.PrepareExecution(codeSlice)
	return state
}

func pushRefTestCode(opcode uint64, reference *cell.Cell) *cell.Cell {
	return cell.BeginCell().MustStoreUInt(opcode, 8).MustStoreRef(reference).EndCell()
}

func pushRefTestLibraryCell(t *testing.T, hash []byte) *cell.Cell {
	t.Helper()

	cl, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(hash, 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to build library cell: %v", err)
	}
	return cl
}

func pushRefTestLibraries(t *testing.T, entries ...*cell.Cell) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(256)
	for _, entry := range entries {
		key := cell.BeginCell().MustStoreSlice(entry.Hash(), 256).EndCell()
		if _, err := dict.SetBuilderWithMode(key, cell.BeginCell().MustStoreRef(entry), cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to add library: %v", err)
		}
	}
	return dict.AsCell()
}

func pushRefTestPrunedCell(t *testing.T) *cell.Cell {
	t.Helper()

	branch := cell.BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		EndCell()
	root := cell.BeginCell().MustStoreUInt(0, 1).MustStoreRef(branch).EndCell()
	proofBuilder := cell.NewMerkleProofBuilder(root)
	if _, err := proofBuilder.Root().BeginParse(); err != nil {
		t.Fatalf("failed to trace proof root: %v", err)
	}
	proof, err := proofBuilder.CreateProof()
	if err != nil {
		t.Fatalf("failed to create proof: %v", err)
	}
	body, err := cell.UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("failed to unwrap proof: %v", err)
	}
	pruned, err := body.PeekRef(0)
	if err != nil {
		t.Fatalf("failed to load pruned ref: %v", err)
	}
	if !pruned.IsSpecial() || pruned.GetType() != cell.PrunedCellType {
		t.Fatalf("expected pruned cell, got special=%v type=%v", pruned.IsSpecial(), pruned.GetType())
	}
	return pruned
}

func assertPushRefSliceCell(t *testing.T, got *cell.Slice, want *cell.Cell) {
	t.Helper()

	gotCell, err := got.ToCell()
	if err != nil {
		t.Fatalf("failed to materialize slice: %v", err)
	}
	if gotCell.HashKey() != want.HashKey() {
		t.Fatalf("slice hash = %x, want %x", gotCell.Hash(), want.Hash())
	}
}

func assertPushRefErrorCode(t *testing.T, err error, want int64) {
	t.Helper()

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != want {
		t.Fatalf("error = %v, want VM error %d", err, want)
	}
}
