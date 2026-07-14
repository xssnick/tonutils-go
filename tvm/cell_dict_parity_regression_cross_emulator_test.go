//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	dictop "github.com/xssnick/tonutils-go/tvm/op/dict"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorCellDictParityRegressions(t *testing.T) {
	fullBuilder := cell.BeginCell().MustStoreSlice(make([]byte, 128), 1023)
	tests := []cellParityCase{
		{name: "stile4_nan_range", code: codeFromBuilders(t, cellsliceop.STILE4().Serialize()), stack: []any{vm.NaN{}, cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		{name: "stule4_nan_range", code: codeFromBuilders(t, cellsliceop.STULE4().Serialize()), stack: []any{vm.NaN{}, cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		{name: "stile8_nan_range", code: codeFromBuilders(t, cellsliceop.STILE8().Serialize()), stack: []any{vm.NaN{}, cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		{name: "stule8_nan_range", code: codeFromBuilders(t, cellsliceop.STULE8().Serialize()), stack: []any{vm.NaN{}, cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		{name: "stile4_capacity_precedes_nan", code: codeFromBuilders(t, cellsliceop.STILE4().Serialize()), stack: []any{vm.NaN{}, fullBuilder.Copy()}, exit: vmerr.CodeCellOverflow},
		{name: "stule4_capacity_precedes_nan", code: codeFromBuilders(t, cellsliceop.STULE4().Serialize()), stack: []any{vm.NaN{}, fullBuilder.Copy()}, exit: vmerr.CodeCellOverflow},
		{name: "stile8_capacity_precedes_nan", code: codeFromBuilders(t, cellsliceop.STILE8().Serialize()), stack: []any{vm.NaN{}, fullBuilder.Copy()}, exit: vmerr.CodeCellOverflow},
		{name: "stule8_capacity_precedes_nan", code: codeFromBuilders(t, cellsliceop.STULE8().Serialize()), stack: []any{vm.NaN{}, fullBuilder.Copy()}, exit: vmerr.CodeCellOverflow},
		{name: "stgrams_nan_range", code: codeFromBuilders(t, cellsliceop.STGRAMS().Serialize()), stack: []any{cell.BeginCell(), vm.NaN{}}, exit: vmerr.CodeRangeCheck},
		{name: "pfxdictswitch_zero_root_flag_dispatches_pfxdictgetq", code: pfxDictSwitchZeroRootFlagCode(0x000), stack: []any{int64(99)}, exit: vmerr.CodeStackUnderflow},
		{name: "pfxdictswitch_zero_root_flag_dispatches_pfxdictget", code: pfxDictSwitchZeroRootFlagCode(0x100), stack: []any{int64(99)}, exit: vmerr.CodeStackUnderflow},
		{name: "pfxdictswitch_zero_root_flag_dispatches_pfxdictgetjmp", code: pfxDictSwitchZeroRootFlagCode(0x200), stack: []any{int64(99)}, exit: vmerr.CodeStackUnderflow},
		{name: "pfxdictswitch_zero_root_flag_dispatches_pfxdictgetexec", code: pfxDictSwitchZeroRootFlagCode(0x300), stack: []any{int64(99)}, exit: vmerr.CodeStackUnderflow},
	}

	runCellParityCases(t, tests)
}

func TestTVMCrossEmulatorPFXDICTSWITCHInstructionGasPrecedesRootLoad(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	code := prependRawMethodDrop(codeFromBuilders(t,
		dictop.PFXDICTSWITCH(cell.BeginCell().EndCell(), 1).Serialize(),
	))
	stackValues := []any{cell.BeginCell().EndCell().MustBeginParse()}

	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("build reference stack: %v", err)
	}

	const gasLimit = int64(51)
	goRes, err := runGoCrossCodeWithGas(code, testEmptyCell(), tuple.Tuple{}, goStack, gasLimit)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCodeWithGas(code, testEmptyCell(), tuple.Tuple{}, refStack, gasLimit)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	wantExit := int32(^vmerr.CodeOutOfGas)
	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("unexpected exit code: go=%d reference=%d want=%d", goRes.exitCode, refRes.exitCode, wantExit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}
	if refRes.gasUsed != 52 {
		t.Fatalf("unexpected reference gas: got=%d want=52", refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}

func TestTVMCrossEmulatorPFXDICTSWITCHRootLoadOutOfGasPrecedesDictionaryResult(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	validRoot := mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
		0b10: {bits: 2, value: codeFromOpcodes(t, 0x79)},
	})
	input := mustSliceKey(t, 0b1011, 4)
	tests := []struct {
		name string
		root *cell.Cell
	}{
		{name: "valid_root", root: validRoot},
		{name: "malformed_empty_root", root: cell.BeginCell().EndCell()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(codeFromBuilders(t,
				dictop.PFXDICTSWITCH(tt.root, 4).Serialize(),
			))
			runPFXDictSwitchOutOfGasParityCase(t, code, []any{input.Copy()}, 60, 152)
		})
	}
}

func runPFXDictSwitchOutOfGasParityCase(t *testing.T, code *cell.Cell, stackValues []any, gasLimit, wantGas int64) {
	t.Helper()

	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithGas(code, testEmptyCell(), tuple.Tuple{}, goStack, gasLimit)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCodeWithGas(code, testEmptyCell(), tuple.Tuple{}, refStack, gasLimit)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	wantExit := int32(^vmerr.CodeOutOfGas)
	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("unexpected exit code: go=%d reference=%d want=%d", goRes.exitCode, refRes.exitCode, wantExit)
	}
	if goRes.gasUsed != refRes.gasUsed || refRes.gasUsed != wantGas {
		t.Fatalf("gas mismatch: go=%d reference=%d want=%d", goRes.gasUsed, refRes.gasUsed, wantGas)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}
