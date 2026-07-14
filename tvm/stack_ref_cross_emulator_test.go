//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorPushRefResolution(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	sliceTarget := cell.BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	contTarget := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(42)).Serialize())
	libraries := mustCrossLibraryCollection(t, sliceTarget, contTarget)
	missingTarget := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	proof := stackRefCrossPrunedProof(t)

	tests := []struct {
		name      string
		code      *cell.Cell
		opcode    uint64
		reference *cell.Cell
		execute   bool
		libraries *cell.Cell
		wantExit  int32
	}{
		{
			name:      "PUSHREFSLICE ordinary",
			opcode:    0x89,
			reference: sliceTarget,
		},
		{
			name:      "PUSHREFCONT ordinary",
			opcode:    0x8A,
			reference: contTarget,
			execute:   true,
		},
		{
			name:      "PUSHREFSLICE library",
			opcode:    0x89,
			reference: mustCrossLibraryCellForHash(t, sliceTarget.Hash()),
			libraries: libraries,
		},
		{
			name:      "PUSHREFCONT library",
			opcode:    0x8A,
			reference: mustCrossLibraryCellForHash(t, contTarget.Hash()),
			execute:   true,
			libraries: libraries,
		},
		{
			name:      "PUSHREFSLICE missing library",
			opcode:    0x89,
			reference: mustCrossLibraryCellForHash(t, missingTarget.Hash()),
			wantExit:  vmerr.CodeCellUnderflow,
		},
		{
			name:      "PUSHREFCONT missing library",
			opcode:    0x8A,
			reference: mustCrossLibraryCellForHash(t, missingTarget.Hash()),
			wantExit:  vmerr.CodeCellUnderflow,
		},
		{
			name:     "PUSHREFSLICE pruned",
			code:     stackRefCrossPrunedExecutionCode(t, proof, 0x89),
			wantExit: vmerr.CodeCellUnderflow,
		},
		{
			name:     "PUSHREFCONT pruned",
			code:     stackRefCrossPrunedExecutionCode(t, proof, 0x8A),
			wantExit: vmerr.CodeCellUnderflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := tt.code
			if code == nil {
				code = stackRefCrossCode(tt.opcode, tt.reference, tt.execute)
			}
			goRes, refRes := runStackRefCrossCase(t, code, tt.libraries, referenceDefaultMaxGas)
			assertStackRefCrossResult(t, goRes, refRes, tt.wantExit)
		})
	}
}

func TestTVMCrossEmulatorPushRefInstructionGasPrecedesCellLoad(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	payload := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	for _, tt := range []struct {
		name   string
		opcode uint64
	}{
		{name: "PUSHREFSLICE", opcode: 0x89},
		{name: "PUSHREFCONT", opcode: 0x8A},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code := stackRefCrossCode(tt.opcode, payload, false)
			const gasLimit = 35 // DROP consumes 18, leaving one less than the PUSHREF* instruction's 18 gas.
			goRes, refRes := runStackRefCrossCase(t, code, nil, gasLimit)
			assertStackRefCrossResult(t, goRes, refRes, int32(^vmerr.CodeOutOfGas))
		})
	}
}

func stackRefCrossCode(opcode uint64, reference *cell.Cell, execute bool) *cell.Cell {
	builder := cell.BeginCell().MustStoreUInt(opcode, 8).MustStoreRef(reference)
	if execute {
		builder.MustStoreBuilder(execop.EXECUTE().Serialize())
	}
	return prependRawMethodDrop(builder.EndCell())
}

func runStackRefCrossCase(t *testing.T, code *cell.Cell, libraries *cell.Cell, gasLimit int64) (*crossRunResult, *crossRunResult) {
	t.Helper()

	var goLibraries []*cell.Cell
	if libraries != nil {
		goLibraries = []*cell.Cell{libraries}
	}
	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, testEmptyCell(), tuple.Tuple{}, goLibraries, vm.NewStack(), referenceRawRunGlobalVersion, gasLimit)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCodeWithLibsAndGas(code, testEmptyCell(), tuple.Tuple{}, libraries, vm.NewStack(), gasLimit)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}
	return goRes, refRes
}

func assertStackRefCrossResult(t *testing.T, goRes, refRes *crossRunResult, wantExit int32) {
	t.Helper()

	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("exit code: go=%d reference=%d want=%d", goRes.exitCode, refRes.exitCode, wantExit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas used: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStack, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStack, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStack.Hash(), refStack.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStack.Dump(), refStack.Dump())
	}
}

func stackRefCrossPrunedProof(t *testing.T) *cell.Cell {
	t.Helper()

	branch := cell.BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		EndCell()
	root := cell.BeginCell().MustStoreUInt(0, 1).MustStoreRef(branch).EndCell()
	proof := mustUsageProofWithLoadedRoot(t, root)
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
	return proof
}

func stackRefCrossPrunedExecutionCode(t *testing.T, proof *cell.Cell, opcode uint64) *cell.Cell {
	t.Helper()

	// Extract a pruned branch from a Merkle proof at runtime, then build and
	// execute a code cell whose PUSHREF* instruction directly references it.
	builders := []*cell.Builder{
		stackop.PUSHREF(proof).Serialize(),
		cellsliceop.XCTOS().Serialize(),
		stackop.DROP().Serialize(),
		cellsliceop.LDREF().Serialize(),
		stackop.DROP().Serialize(),
		cellsliceop.CTOS().Serialize(),
		cellsliceop.LDREF().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHINT(new(big.Int).SetUint64(opcode)).Serialize(),
		cellsliceop.NEWC().Serialize(),
		cellsliceop.STU(8).Serialize(),
		cellsliceop.STREF().Serialize(),
		cellsliceop.ENDC().Serialize(),
		cellsliceop.CTOS().Serialize(),
		execop.BLESS().Serialize(),
		execop.EXECUTE().Serialize(),
	}
	return prependRawMethodDrop(codeFromBuilders(t, builders...))
}
