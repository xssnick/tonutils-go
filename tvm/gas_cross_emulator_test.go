//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorGasBeyondOutOfGas(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	intCode := func(v int64) *cell.Builder {
		return stackop.PUSHINT(big.NewInt(v)).Serialize()
	}
	body := func(builders ...*cell.Builder) *cell.Cell {
		return codeFromBuilders(t, builders...)
	}

	tupleHeavyStack := make([]any, 0, 15)
	for i := 0; i < cap(tupleHeavyStack); i++ {
		tupleHeavyStack = append(tupleHeavyStack, int64(i+1))
	}

	callCCCopyStack := make([]any, 0, 34)
	callCCCopyStack = append(callCCCopyStack, int64(99))
	for i := int64(0); i < 33; i++ {
		callCCCopyStack = append(callCCCopyStack, 100+i)
	}

	throwArgBody := body(
		intCode(321),
		cell.BeginCell().MustStoreUInt(0xF2C955, 24),
	)
	exceptionTupleHandler := body(
		tupleop.TUPLE(15).Serialize(),
		tupleop.EXPLODE(15).Serialize(),
	)
	stackGrowthBody := body(
		intCode(1), intCode(2), intCode(3), intCode(4),
		intCode(5), intCode(6), intCode(7), intCode(8),
		intCode(9), intCode(10), intCode(11), intCode(12),
		intCode(13), intCode(14), intCode(15), intCode(16),
		intCode(17), intCode(18), intCode(19), intCode(20),
	)

	type testCase struct {
		name     string
		code     *cell.Cell
		stack    []any
		gasLimit int64
	}

	tests := []testCase{
		{
			name: "tuple_build_overruns_limit",
			code: prependRawMethodDrop(body(
				tupleop.TUPLE(15).Serialize(),
				tupleop.EXPLODE(15).Serialize(),
			)),
			stack:    tupleHeavyStack,
			gasLimit: 45,
		},
		{
			name:     "stack_growth_overruns_limit",
			code:     prependRawMethodDrop(stackGrowthBody),
			gasLimit: 115,
		},
		{
			name: "callccargs_stack_copy_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(body()).Serialize(),
				execop.CALLCCARGS(33, 0).Serialize(),
			)),
			stack:    callCCCopyStack,
			gasLimit: 30,
		},
		{
			name: "callccvarargs_stack_copy_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(body()).Serialize(),
				intCode(33),
				intCode(0),
				execop.CALLCCVARARGS().Serialize(),
			)),
			stack:    callCCCopyStack,
			gasLimit: 50,
		},
		{
			name: "tryargs_stack_copy_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(body()).Serialize(),
				stackop.PUSHCONT(body()).Serialize(),
				execop.TRYARGS(33, 0).Serialize(),
			)),
			stack:    callCCCopyStack,
			gasLimit: 40,
		},
		{
			name: "setcontvarargs_stack_copy_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(body()).Serialize(),
				intCode(33),
				intCode(-1),
				execop.SETCONTVARARGS().Serialize(),
			)),
			stack:    callCCCopyStack,
			gasLimit: 50,
		},
		{
			name:     "returnargs_stack_copy_overruns_limit",
			code:     prependRawMethodDrop(body(execop.RETURNARGS(1).Serialize())),
			stack:    callCCCopyStack,
			gasLimit: 20,
		},
		{
			name: "blessvarargs_stack_copy_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHSLICEINLINE(body().MustBeginParse()).Serialize(),
				intCode(33),
				intCode(0),
				execop.BLESSVARARGS().Serialize(),
			)),
			stack:    callCCCopyStack,
			gasLimit: 60,
		},
		{
			name:     "cell_create_overruns_before_endc_push",
			code:     body(cellsliceop.ENDC().Serialize()),
			stack:    []any{cell.BeginCell().MustStoreUInt(0xA, 4)},
			gasLimit: 20,
		},
		{
			name:     "cell_create_overruns_before_endxc_finalize",
			code:     prependRawMethodDrop(body(cellsliceop.ENDXC().Serialize())),
			stack:    []any{cell.BeginCell(), int64(0)},
			gasLimit: 200,
		},
		{
			name: "exception_handler_tuple_gas_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(throwArgBody).Serialize(),
				stackop.PUSHCONT(exceptionTupleHandler).Serialize(),
				execop.TRY().Serialize(),
			)),
			stack:    tupleHeavyStack,
			gasLimit: 155,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCodeWithGas(tt.code, testEmptyCell(), tuple.Tuple{}, goStack, tt.gasLimit)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCodeWithGas(tt.code, testEmptyCell(), tuple.Tuple{}, refStack, tt.gasLimit)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != int32(^vmerr.CodeOutOfGas) || refRes.exitCode != int32(^vmerr.CodeOutOfGas) {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, ^vmerr.CodeOutOfGas)
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}

			goStackCell, err := normalizeStackCell(goRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize go stack: %v", err)
			}
			refStackCell, err := normalizeStackCell(refRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize reference stack: %v", err)
			}
			if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
			}
		})
	}
}

func TestTVMCrossEmulatorImplicitJmprefGasBeforeRefLoad(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	code := cell.BeginCell().
		MustStoreRef(testEmptyCell()).
		EndCell()

	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithGas(code, testEmptyCell(), tuple.Tuple{}, goStack, 50)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCodeWithGas(code, testEmptyCell(), tuple.Tuple{}, refStack, 50)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != int32(^vmerr.CodeOutOfGas) || refRes.exitCode != int32(^vmerr.CodeOutOfGas) {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, ^vmerr.CodeOutOfGas)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}
}

func TestTVMCrossEmulatorGasRepeatedCellLoads(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	loadAndDrop := func(c *cell.Cell) []*cell.Builder {
		return []*cell.Builder{
			stackop.PUSHREF(c).Serialize(),
			cellsliceop.CTOS().Serialize(),
			stackop.DROP().Serialize(),
		}
	}
	codeFromParts := func(parts ...[]*cell.Builder) *cell.Cell {
		var builders []*cell.Builder
		for _, part := range parts {
			builders = append(builders, part...)
		}
		return prependRawMethodDrop(codeFromBuilders(t, builders...))
	}

	sharedCell := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	firstCell := cell.BeginCell().MustStoreUInt(0x11, 8).EndCell()
	secondCell := cell.BeginCell().MustStoreUInt(0x22, 8).EndCell()
	thirdCell := cell.BeginCell().MustStoreUInt(0x33, 8).EndCell()

	tests := []struct {
		name string
		code *cell.Cell
	}{
		{
			name: "same_cell_loaded_three_times",
			code: codeFromParts(loadAndDrop(sharedCell), loadAndDrop(sharedCell), loadAndDrop(sharedCell)),
		},
		{
			name: "distinct_cells_loaded_once_each",
			code: codeFromParts(loadAndDrop(firstCell), loadAndDrop(secondCell), loadAndDrop(thirdCell)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goStack, err := buildCrossStack()
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack()
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCode(tt.code, testEmptyCell(), tuple.Tuple{}, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(tt.code, testEmptyCell(), tuple.Tuple{}, refStack)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goRes.stack.Dump(), refRes.stack.Dump())
			}
		})
	}
}

func TestTVMCrossEmulatorGasAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := gasVersionedParityCases(t, 0xAB)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_GAS_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runGasVersionedParityCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorGasGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%gasVersionedParityCaseCount), uint16(0xAB+version), uint32(1000))
	}
	defaultCases := gasVersionedParityCasesForSeeds()
	for i, gasLimit := range defaultCases {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i), uint16(0x20+i), uint32(gasLimit))
	}
	f.Add(uint8(255), uint8(255), uint16(0xffff), uint32(1))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8, rawValue uint16, rawGas uint32) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := gasVersionedParityCases(t, rawValue)
		if len(tests) != gasVersionedParityCaseCount {
			t.Fatalf("gas versioned parity case count = %d, want %d", len(tests), gasVersionedParityCaseCount)
		}

		tt := tests[int(rawCase)%len(tests)]
		tt.gasLimit = int64(1 + rawGas%uint32(tt.gasLimit))
		tt.checkExit = false
		runGasVersionedParityCase(t, tt, version)
	})
}

func gasCrossCode(t *testing.T, instructions ...*cell.Builder) *cell.Cell {
	t.Helper()

	return buildStackProgram(t, instructions)
}

type gasVersionedParityCase struct {
	name      string
	code      *cell.Cell
	stack     []any
	gasLimit  int64
	wantExit  int32
	checkExit bool
}

const gasVersionedParityCaseCount = 14

func gasVersionedParityCasesForSeeds() []int64 {
	return []int64{
		100_000,
		100_000,
		115,
		45,
		30,
		50,
		40,
		50,
		20,
		60,
		50,
		200,
		20,
		155,
	}
}

func gasVersionedParityCases(t *testing.T, value uint16) []gasVersionedParityCase {
	t.Helper()

	intCode := func(v int64) *cell.Builder {
		return stackop.PUSHINT(big.NewInt(v)).Serialize()
	}
	body := func(builders ...*cell.Builder) *cell.Cell {
		return codeFromBuilders(t, builders...)
	}

	tupleHeavyStack := make([]any, 0, 15)
	for i := int64(0); i < 15; i++ {
		tupleHeavyStack = append(tupleHeavyStack, int64(value)+i+1)
	}

	callCCCopyStack := make([]any, 0, 34)
	callCCCopyStack = append(callCCCopyStack, int64(value)+99)
	for i := int64(0); i < 33; i++ {
		callCCCopyStack = append(callCCCopyStack, int64(value)+100+i)
	}

	sharedCell := cell.BeginCell().MustStoreUInt(uint64(value&0xff), 8).EndCell()
	firstCell := cell.BeginCell().MustStoreUInt(uint64(value&0xff), 8).EndCell()
	secondCell := cell.BeginCell().MustStoreUInt(uint64((value+1)&0xff), 8).EndCell()
	thirdCell := cell.BeginCell().MustStoreUInt(uint64((value+2)&0xff), 8).EndCell()

	loadAndDrop := func(c *cell.Cell) []*cell.Builder {
		return []*cell.Builder{
			stackop.PUSHREF(c).Serialize(),
			cellsliceop.CTOS().Serialize(),
			stackop.DROP().Serialize(),
		}
	}
	codeFromParts := func(parts ...[]*cell.Builder) *cell.Cell {
		var builders []*cell.Builder
		for _, part := range parts {
			builders = append(builders, part...)
		}
		return gasCrossCode(t, builders...)
	}

	stackGrowth := make([]*cell.Builder, 0, 20)
	for i := int64(1); i <= 20; i++ {
		stackGrowth = append(stackGrowth, intCode(int64(value)+i))
	}

	throwArgBody := body(
		intCode(int64(value)+321),
		cell.BeginCell().MustStoreUInt(0xF2C955, 24),
	)
	exceptionTupleHandler := body(
		tupleop.TUPLE(15).Serialize(),
		tupleop.EXPLODE(15).Serialize(),
	)

	oogExit := int32(^vmerr.CodeOutOfGas)
	return []gasVersionedParityCase{
		{
			name:      "repeated_cell_loads",
			code:      codeFromParts(loadAndDrop(sharedCell), loadAndDrop(sharedCell), loadAndDrop(sharedCell)),
			gasLimit:  100_000,
			checkExit: true,
		},
		{
			name:      "distinct_cell_loads",
			code:      codeFromParts(loadAndDrop(firstCell), loadAndDrop(secondCell), loadAndDrop(thirdCell)),
			gasLimit:  100_000,
			checkExit: true,
		},
		{
			name:      "stack_growth_out_of_gas",
			code:      gasCrossCode(t, stackGrowth...),
			gasLimit:  115,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name: "tuple_build_overruns_limit",
			code: prependRawMethodDrop(body(
				tupleop.TUPLE(15).Serialize(),
				tupleop.EXPLODE(15).Serialize(),
			)),
			stack:     tupleHeavyStack,
			gasLimit:  45,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name: "callccargs_stack_copy_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(body()).Serialize(),
				execop.CALLCCARGS(33, 0).Serialize(),
			)),
			stack:     callCCCopyStack,
			gasLimit:  30,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name: "callccvarargs_stack_copy_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(body()).Serialize(),
				intCode(33),
				intCode(0),
				execop.CALLCCVARARGS().Serialize(),
			)),
			stack:     callCCCopyStack,
			gasLimit:  50,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name: "tryargs_stack_copy_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(body()).Serialize(),
				stackop.PUSHCONT(body()).Serialize(),
				execop.TRYARGS(33, 0).Serialize(),
			)),
			stack:     callCCCopyStack,
			gasLimit:  40,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name: "setcontvarargs_stack_copy_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(body()).Serialize(),
				intCode(33),
				intCode(-1),
				execop.SETCONTVARARGS().Serialize(),
			)),
			stack:     callCCCopyStack,
			gasLimit:  50,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name:      "cell_create_overruns_before_endc_push",
			code:      body(cellsliceop.ENDC().Serialize()),
			stack:     []any{cell.BeginCell().MustStoreUInt(uint64(value&0xf), 4)},
			gasLimit:  20,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name: "blessvarargs_stack_copy_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHSLICEINLINE(body().MustBeginParse()).Serialize(),
				intCode(33),
				intCode(0),
				execop.BLESSVARARGS().Serialize(),
			)),
			stack:     callCCCopyStack,
			gasLimit:  60,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name:      "implicit_jmpref_gas_before_ref_load",
			code:      cell.BeginCell().MustStoreRef(testEmptyCell()).EndCell(),
			gasLimit:  50,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name:      "cell_create_overruns_before_endxc_finalize",
			code:      gasCrossCode(t, cellsliceop.ENDXC().Serialize()),
			stack:     []any{cell.BeginCell(), int64(0)},
			gasLimit:  200,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name:      "returnargs_stack_copy_overruns_limit",
			code:      prependRawMethodDrop(body(execop.RETURNARGS(1).Serialize())),
			stack:     callCCCopyStack,
			gasLimit:  20,
			wantExit:  oogExit,
			checkExit: true,
		},
		{
			name: "exception_handler_tuple_gas_overruns_limit",
			code: prependRawMethodDrop(body(
				stackop.PUSHCONT(throwArgBody).Serialize(),
				stackop.PUSHCONT(exceptionTupleHandler).Serialize(),
				execop.TRY().Serialize(),
			)),
			stack:     tupleHeavyStack,
			gasLimit:  155,
			wantExit:  oogExit,
			checkExit: true,
		},
	}
}

func runGasVersionedParityCase(t *testing.T, tt gasVersionedParityCase, globalVersion int) {
	t.Helper()

	goStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersionGasAndLibs(tt.code, testEmptyCell(), tuple.Tuple{}, nil, goStack, globalVersion, tt.gasLimit)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refCfg.GasLimit = tt.gasLimit
	refRes, err := runReferenceCrossCodeViaEmulator(tt.code, testEmptyCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if tt.checkExit && goRes.exitCode != tt.wantExit {
		t.Fatalf("unexpected exit code: got=%d expected=%d", goRes.exitCode, tt.wantExit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}
