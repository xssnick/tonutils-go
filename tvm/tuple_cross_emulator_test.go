//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorTupleOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	twoTuple := tuplepkg.NewTupleValue(big.NewInt(7), big.NewInt(8))

	type testCase struct {
		name  string
		code  *cell.Cell
		stack []any
		exit  int32
	}

	tests := []testCase{
		{
			name: "pushnull_isnull",
			code: codeFromBuilders(t, tupleop.PUSHNULL().Serialize(), tupleop.ISNULL().Serialize()),
			exit: 0,
		},
		{
			name: "zero_tuple_is_not_null",
			code: codeFromBuilders(t, tupleop.TUPLE(0).Serialize(), tupleop.ISNULL().Serialize()),
			exit: 0,
		},
		{
			name:  "nan_isnull",
			code:  codeFromBuilders(t, tupleop.ISNULL().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  0,
		},
		{
			name:  "nan_istuple",
			code:  codeFromBuilders(t, tupleop.ISTUPLE().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  0,
		},
		{
			name:  "nan_qtlen",
			code:  codeFromBuilders(t, tupleop.QTLEN().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  0,
		},
		{
			name:  "indexq_nan_value",
			code:  codeFromBuilders(t, tupleop.INDEXQ(0).Serialize()),
			stack: []any{tuplepkg.NewTupleValue(vm.NaN{}, nil)},
			exit:  0,
		},
		{
			name:  "untuple_nan_and_null",
			code:  codeFromBuilders(t, tupleop.UNTUPLE(2).Serialize()),
			stack: []any{tuplepkg.NewTupleValue(vm.NaN{}, nil)},
			exit:  0,
		},
		{
			name:  "setindexq_nil_tuple_nan_value",
			code:  codeFromBuilders(t, tupleop.SETINDEXQ(1).Serialize()),
			stack: []any{nil, vm.NaN{}},
			exit:  0,
		},
		{
			name:  "setindexq_empty_tuple_nil_value",
			code:  codeFromBuilders(t, tupleop.SETINDEXQ(0).Serialize()),
			stack: []any{tuplepkg.NewTupleValue(), nil},
			exit:  0,
		},
		{
			name:  "indexq_out_of_range_nil",
			code:  codeFromBuilders(t, tupleop.INDEXQ(5).Serialize()),
			stack: []any{twoTuple},
			exit:  0,
		},
		{
			name:  "indexvarq_out_of_range_nil",
			code:  codeFromBuilders(t, tupleop.INDEXVARQ().Serialize()),
			stack: []any{twoTuple, int64(5)},
			exit:  0,
		},
		{
			name:  "setindexvarq_nil_tuple_nil_value",
			code:  codeFromBuilders(t, tupleop.SETINDEXVARQ().Serialize()),
			stack: []any{nil, nil, int64(1)},
			exit:  0,
		},
		{
			name:  "qtlen_non_tuple",
			code:  codeFromBuilders(t, tupleop.QTLEN().Serialize()),
			stack: []any{int64(10)},
			exit:  0,
		},
		{
			name:  "istuple_non_tuple",
			code:  codeFromBuilders(t, tupleop.ISTUPLE().Serialize()),
			stack: []any{int64(1)},
			exit:  0,
		},
		{
			name:  "last_non_tuple_typecheck",
			code:  codeFromBuilders(t, tupleop.LAST().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "untuple_exact_arity_mismatch",
			code:  codeFromBuilders(t, tupleop.UNTUPLE(3).Serialize()),
			stack: []any{twoTuple},
			exit:  vmerr.CodeTypeCheck,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(tt.code)

			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCode(code, testEmptyCell(), tuplepkg.Tuple{}, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(code, testEmptyCell(), tuplepkg.Tuple{}, refStack)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
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

func TestTVMCrossEmulatorTupleOpsOpcodeSpace(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := buildTupleOpsOpcodeSpaceCases()
	if len(cases) != tupleOpsOpcodeSpaceCaseCount {
		t.Fatalf("tuple opcode-space case count = %d, want %d", len(cases), tupleOpsOpcodeSpaceCaseCount)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runTupleOpParityCase(t, tc)
		})
	}
}

func TestTVMCrossEmulatorTupleOpsOpcodeSpaceAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := buildTupleOpsOpcodeSpaceCases()
	if len(cases) != tupleOpsOpcodeSpaceCaseCount {
		t.Fatalf("tuple opcode-space case count = %d, want %d", len(cases), tupleOpsOpcodeSpaceCaseCount)
	}
	versions := crossEmulatorVersionAuditVersions(t, "TVM_TUPLE_OPCODE_SPACE_VERSION_AUDIT")
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runTupleOpVersionedParityCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTupleOpsOpcodeSpaceGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(version%tupleOpsOpcodeSpaceCaseCount))
	}
	for caseIdx := 0; caseIdx < tupleOpsOpcodeSpaceCaseCount; caseIdx++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint16(caseIdx))
	}
	f.Add(uint8(255), uint16(65535))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		cases := buildTupleOpsOpcodeSpaceCases()
		if len(cases) != tupleOpsOpcodeSpaceCaseCount {
			t.Fatalf("tuple opcode-space case count = %d, want %d", len(cases), tupleOpsOpcodeSpaceCaseCount)
		}
		tt := cases[int(rawCase)%len(cases)]
		runTupleOpVersionedParityCase(t, tt, version)
	})
}

func TestTVMCrossEmulatorTupleOpsDynamicAndErrors(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, tt := range buildTupleOpsDynamicAndErrorCases() {
		t.Run(tt.name, func(t *testing.T) {
			runTupleOpParityCase(t, tt)
		})
	}
}

func TestTVMCrossEmulatorTupleOpsDynamicAndErrorsAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := buildTupleOpsDynamicAndErrorCases()
	if len(cases) != tupleOpsDynamicAndErrorCaseCount {
		t.Fatalf("tuple dynamic/error case count = %d, want %d", len(cases), tupleOpsDynamicAndErrorCaseCount)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_TUPLE_DYNAMIC_VERSION_AUDIT")
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runTupleOpVersionedParityCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTupleOpsDynamicAndErrorsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%tupleOpsDynamicAndErrorCaseCount))
	}
	for caseIdx := 0; caseIdx < tupleOpsDynamicAndErrorCaseCount; caseIdx++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(caseIdx))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		cases := buildTupleOpsDynamicAndErrorCases()
		if len(cases) != tupleOpsDynamicAndErrorCaseCount {
			t.Fatalf("tuple dynamic/error case count = %d, want %d", len(cases), tupleOpsDynamicAndErrorCaseCount)
		}
		tt := cases[int(rawCase)%len(cases)]
		runTupleOpVersionedParityCase(t, tt, version)
	})
}

const tupleOpsOpcodeSpaceCaseCount = 243
const tupleOpsDynamicAndErrorCaseCount = 64

func TestTVMCrossEmulatorTupleOpsLowGasStackEffects(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := tupleOpsLowGasStackEffectCases(t)
	if len(tests) != tupleOpsLowGasStackEffectCaseCount {
		t.Fatalf("tuple low-gas stack-effect case count = %d, want %d", len(tests), tupleOpsLowGasStackEffectCaseCount)
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runTupleOpLowGasStackEffectCase(t, tt)
		})
	}
}

func TestTVMCrossEmulatorTupleOpsLowGasStackEffectsAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := tupleOpsLowGasStackEffectCases(t)
	if len(tests) != tupleOpsLowGasStackEffectCaseCount {
		t.Fatalf("tuple low-gas stack-effect case count = %d, want %d", len(tests), tupleOpsLowGasStackEffectCaseCount)
	}
	versions := crossEmulatorVersionAuditVersions(t, "TVM_TUPLE_LOW_GAS_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runTupleOpLowGasStackEffectVersionedCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTupleOpsLowGasStackEffectsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%tupleOpsLowGasStackEffectCaseCount), uint8(25))
	}
	for caseIdx := 0; caseIdx < tupleOpsLowGasStackEffectCaseCount; caseIdx++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(caseIdx), uint8(25))
	}
	f.Add(uint8(255), uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8, rawGas uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := tupleOpsLowGasStackEffectCases(t)
		if len(tests) != tupleOpsLowGasStackEffectCaseCount {
			t.Fatalf("tuple low-gas stack-effect case count = %d, want %d", len(tests), tupleOpsLowGasStackEffectCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		tt.gasLimit = 1 + int64(rawGas%32)
		if tt.gasLimit > 25 {
			tt.gasLimit = 25
		}
		runTupleOpLowGasStackEffectVersionedCase(t, tt, version)
	})
}

const tupleOpsLowGasStackEffectCaseCount = 6

type tupleOpsLowGasStackEffectCase struct {
	name     string
	code     *cell.Cell
	stack    []any
	gasLimit int64
}

func tupleOpsLowGasStackEffectCases(t *testing.T) []tupleOpsLowGasStackEffectCase {
	t.Helper()

	return []tupleOpsLowGasStackEffectCase{
		{
			name:     "explode_out_of_gas",
			code:     prependRawMethodDrop(codeFromBuilders(t, tupleop.EXPLODE(2).Serialize())),
			stack:    []any{tuplepkg.NewTupleValue(big.NewInt(4), big.NewInt(5))},
			gasLimit: 25,
		},
		{
			name:     "untuple_out_of_gas",
			code:     prependRawMethodDrop(codeFromBuilders(t, tupleop.UNTUPLE(2).Serialize())),
			stack:    []any{tuplepkg.NewTupleValue(big.NewInt(6), big.NewInt(7))},
			gasLimit: 25,
		},
		{
			name:     "unpackfirst_out_of_gas",
			code:     prependRawMethodDrop(codeFromBuilders(t, tupleop.UNPACKFIRST(2).Serialize())),
			stack:    []any{tuplepkg.NewTupleValue(big.NewInt(8), big.NewInt(9), big.NewInt(10))},
			gasLimit: 25,
		},
		{
			name:     "explodevar_out_of_gas",
			code:     prependRawMethodDrop(codeFromBuilders(t, tupleop.EXPLODEVAR().Serialize())),
			stack:    []any{tuplepkg.NewTupleValue(big.NewInt(11), big.NewInt(12)), int64(2)},
			gasLimit: 25,
		},
		{
			name:     "untuplevar_out_of_gas",
			code:     prependRawMethodDrop(codeFromBuilders(t, tupleop.UNTUPLEVAR().Serialize())),
			stack:    []any{tuplepkg.NewTupleValue(big.NewInt(13), big.NewInt(14)), int64(2)},
			gasLimit: 25,
		},
		{
			name:     "unpackfirstvar_out_of_gas",
			code:     prependRawMethodDrop(codeFromBuilders(t, tupleop.UNPACKFIRSTVAR().Serialize())),
			stack:    []any{tuplepkg.NewTupleValue(big.NewInt(15), big.NewInt(16), big.NewInt(17)), int64(2)},
			gasLimit: 25,
		},
	}
}

func runTupleOpLowGasStackEffectCase(t *testing.T, tt tupleOpsLowGasStackEffectCase) {
	t.Helper()

	goStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithGas(tt.code, testEmptyCell(), tuplepkg.Tuple{}, goStack, tt.gasLimit)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCodeWithGas(tt.code, testEmptyCell(), tuplepkg.Tuple{}, refStack, tt.gasLimit)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	assertTupleLowGasStackEffectResult(t, goRes, refRes)
}

func runTupleOpLowGasStackEffectVersionedCase(t *testing.T, tt tupleOpsLowGasStackEffectCase, globalVersion int) {
	t.Helper()

	goStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersionGasAndLibs(tt.code, testEmptyCell(), tuplepkg.Tuple{}, nil, goStack, globalVersion, tt.gasLimit)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refCfg.GasLimit = tt.gasLimit
	refRes, err := runReferenceCrossCodeViaEmulator(tt.code, testEmptyCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	assertTupleLowGasStackEffectResult(t, goRes, refRes)
}

func assertTupleLowGasStackEffectResult(t *testing.T, goRes, refRes *crossRunResult) {
	t.Helper()

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
}

func TestTVMCrossEmulatorTupleOpsVersionInvariance(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := tupleOpsVersionInvarianceCases(t)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_TUPLE_VERSION_AUDIT")
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runTupleOpVersionedParityCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTupleOpsVersionInvariance(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%tupleOpsVersionInvarianceCaseCount))
	}
	for i := 0; i < tupleOpsVersionInvarianceCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		cases := tupleOpsVersionInvarianceCases(t)
		if len(cases) != tupleOpsVersionInvarianceCaseCount {
			t.Fatalf("tuple version-invariance case count = %d, want %d", len(cases), tupleOpsVersionInvarianceCaseCount)
		}
		tt := cases[int(rawCase)%len(cases)]
		runTupleOpVersionedParityCase(t, tt, version)
	})
}

const tupleOpsVersionInvarianceCaseCount = 14

type tupleOpParityCase struct {
	name  string
	op    *cell.Builder
	stack []any
	exit  int32
}

func tupleOpsVersionInvarianceCases(t *testing.T) []tupleOpParityCase {
	t.Helper()

	return []tupleOpParityCase{
		{name: "pushnull_isnull", op: codeFromBuilders(t, tupleop.PUSHNULL().Serialize(), tupleop.ISNULL().Serialize()).ToBuilder()},
		{name: "indexq_oob_nil", op: tupleRawOp(0x6f65, 16), stack: []any{tupleParityInts(7000, 2)}},
		{name: "setindexvarq_nil_tuple_extends", op: tupleRawOp(0x6f87, 16), stack: []any{nil, big.NewInt(888), int64(3)}},
		{name: "setindexvarq_sparse_nil_tuple_nil_value", op: tupleRawOp(0x6f87, 16), stack: []any{nil, nil, int64(254)}},
		{name: "qtlen_nan_minus_one", op: tupleRawOp(0x6f89, 16), stack: []any{vm.NaN{}}},
		{name: "index2_nested", op: tupleRawOp(0x6fb5, 16), stack: []any{tupleParityNested2()}},
		{name: "index3_final_oob_range", op: tupleRawOp(0x6fc1, 16), stack: []any{tuplepkg.NewTupleValue(tuplepkg.NewTupleValue(tupleParityInts(26200, 1)))}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "tuplevar_255_boundary", op: tupleRawOp(0x6f80, 16), stack: append(tupleParityValues(10000, 255), int64(255))},
		{name: "tpop_empty_typecheck", op: tupleRawOp(0x6f8d, 16), stack: []any{tupleParityInts(24000, 0)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "tpush_too_large", op: tupleRawOp(0x6f8c, 16), stack: []any{tupleParityInts(23000, 255), big.NewInt(4)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "indexvar_255_range", op: tupleRawOp(0x6f81, 16), stack: []any{tupleParityInts(27000, 255), int64(255)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "nullswapif_non_integer", op: tupleRawOp(0x6fa0, 16), stack: []any{nil}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "invalid_6f9f_gap", op: tupleRawOp(0x6f9f, 16), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "invalid_6fa8_gap", op: tupleRawOp(0x6fa8, 16), exit: int32(vmerr.CodeInvalidOpcode)},
	}
}

func buildTupleOpsOpcodeSpaceCases() []tupleOpParityCase {
	var cases []tupleOpParityCase
	add := func(name string, op *cell.Builder, stack []any) {
		cases = append(cases, tupleOpParityCase{name: name, op: op, stack: stack})
	}

	add("pushnull", tupleRawOp(0x6d, 8), nil)
	add("isnull_null", tupleRawOp(0x6e, 8), []any{nil})
	add("isnull_int", tupleRawOp(0x6e, 8), []any{int64(1)})

	for n := 0; n < 16; n++ {
		add(fmt.Sprintf("tuple_%d", n), tupleRawOp(0x6f00|uint64(n), 16), tupleParityValues(100*n, n))
		add(fmt.Sprintf("index_%d", n), tupleRawOp(0x6f10|uint64(n), 16), []any{tupleParityInts(1000+n*20, 16)})
		add(fmt.Sprintf("untuple_%d", n), tupleRawOp(0x6f20|uint64(n), 16), []any{tupleParityInts(2000+n*20, n)})
		add(fmt.Sprintf("unpackfirst_%d", n), tupleRawOp(0x6f30|uint64(n), 16), []any{tupleParityInts(3000+n*20, 16)})
		add(fmt.Sprintf("explode_%d", n), tupleRawOp(0x6f40|uint64(n), 16), []any{tupleParityInts(4000+n*20, n)})
		add(fmt.Sprintf("setindex_%d", n), tupleRawOp(0x6f50|uint64(n), 16), []any{tupleParityInts(5000+n*20, 16), big.NewInt(int64(6000 + n))})
		add(fmt.Sprintf("indexq_%d", n), tupleRawOp(0x6f60|uint64(n), 16), []any{tupleParityInts(7000+n*20, 16)})
		add(fmt.Sprintf("setindexq_%d", n), tupleRawOp(0x6f70|uint64(n), 16), []any{tupleParityInts(8000+n*20, 16), big.NewInt(int64(9000 + n))})
	}

	add("tuplevar_4", tupleRawOp(0x6f80, 16), append(tupleParityValues(10000, 4), int64(4)))
	add("indexvar_3", tupleRawOp(0x6f81, 16), []any{tupleParityInts(10100, 8), int64(3)})
	add("untuplevar_4", tupleRawOp(0x6f82, 16), []any{tupleParityInts(10200, 4), int64(4)})
	add("unpackfirstvar_4", tupleRawOp(0x6f83, 16), []any{tupleParityInts(10300, 8), int64(4)})
	add("explodevar_4", tupleRawOp(0x6f84, 16), []any{tupleParityInts(10400, 4), int64(4)})
	add("setindexvar_3", tupleRawOp(0x6f85, 16), []any{tupleParityInts(10500, 8), big.NewInt(7777), int64(3)})
	add("indexvarq_3", tupleRawOp(0x6f86, 16), []any{tupleParityInts(10600, 8), int64(3)})
	add("setindexvarq_3", tupleRawOp(0x6f87, 16), []any{tupleParityInts(10700, 8), big.NewInt(8888), int64(3)})
	add("tlen", tupleRawOp(0x6f88, 16), []any{tupleParityInts(10800, 8)})
	add("qtlen_tuple", tupleRawOp(0x6f89, 16), []any{tupleParityInts(10900, 8)})
	add("qtlen_non_tuple", tupleRawOp(0x6f89, 16), []any{int64(1)})
	add("istuple_tuple", tupleRawOp(0x6f8a, 16), []any{tupleParityInts(11000, 8)})
	add("istuple_non_tuple", tupleRawOp(0x6f8a, 16), []any{int64(1)})
	add("last", tupleRawOp(0x6f8b, 16), []any{tupleParityInts(11100, 8)})
	add("tpush", tupleRawOp(0x6f8c, 16), []any{tupleParityInts(11200, 8), big.NewInt(12345)})
	add("tpop", tupleRawOp(0x6f8d, 16), []any{tupleParityInts(11300, 8)})

	for op := uint64(0x6fa0); op <= 0x6fa7; op++ {
		add(fmt.Sprintf("nullop_%04x_zero", op), tupleRawOp(op, 16), tupleNullOpStack(op, 0))
		add(fmt.Sprintf("nullop_%04x_nonzero", op), tupleRawOp(op, 16), tupleNullOpStack(op, 1))
	}

	nested2 := tupleParityNested2()
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			add(fmt.Sprintf("index2_%d_%d", i, j), tupleRawOp(0x6fb0|uint64(i<<2)|uint64(j), 16), []any{nested2})
		}
	}

	nested3 := tupleParityNested3()
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			for k := 0; k < 4; k++ {
				add(fmt.Sprintf("index3_%d_%d_%d", i, j, k), tupleRawOp(0x6fc0|uint64(i<<4)|uint64(j<<2)|uint64(k), 16), []any{nested3})
			}
		}
	}

	return cases
}

func buildTupleOpsDynamicAndErrorCases() []tupleOpParityCase {
	return []tupleOpParityCase{
		{name: "tuplevar_0", op: tupleRawOp(0x6f80, 16), stack: []any{int64(0)}},
		{name: "tuplevar_255", op: tupleRawOp(0x6f80, 16), stack: append(tupleParityValues(10000, 255), int64(255))},
		{name: "indexvar_254", op: tupleRawOp(0x6f81, 16), stack: []any{tupleParityInts(11000, 255), int64(254)}},
		{name: "untuplevar_255", op: tupleRawOp(0x6f82, 16), stack: []any{tupleParityInts(12000, 255), int64(255)}},
		{name: "unpackfirstvar_255", op: tupleRawOp(0x6f83, 16), stack: []any{tupleParityInts(13000, 255), int64(255)}},
		{name: "explodevar_255", op: tupleRawOp(0x6f84, 16), stack: []any{tupleParityInts(14000, 255), int64(255)}},
		{name: "setindexvar_254", op: tupleRawOp(0x6f85, 16), stack: []any{tupleParityInts(15000, 255), big.NewInt(777), int64(254)}},
		{name: "indexvarq_254", op: tupleRawOp(0x6f86, 16), stack: []any{tupleParityInts(16000, 255), int64(254)}},
		{name: "setindexvarq_254_nil_tuple_value", op: tupleRawOp(0x6f87, 16), stack: []any{nil, big.NewInt(888), int64(254)}},
		{name: "setindexvarq_254_nil_tuple_nil_value", op: tupleRawOp(0x6f87, 16), stack: []any{nil, nil, int64(254)}},
		{name: "setindexvarq_254_existing_nil_fill", op: tupleRawOp(0x6f87, 16), stack: []any{tupleParityInts(17000, 1), nil, int64(254)}},
		{name: "indexq_nil_tuple_null", op: tupleRawOp(0x6f60, 16), stack: []any{nil}},
		{name: "indexvarq_nil_tuple_null", op: tupleRawOp(0x6f86, 16), stack: []any{nil, int64(0)}},
		{name: "setindexq_nil_tuple_allocates", op: tupleRawOp(0x6f73, 16), stack: []any{nil, big.NewInt(77)}},
		{name: "setindexq_nil_tuple_nil_stays_null", op: tupleRawOp(0x6f73, 16), stack: []any{nil, nil}},
		{name: "setindexq_existing_oob_extends", op: tupleRawOp(0x6f73, 16), stack: []any{tupleParityInts(17500, 1), big.NewInt(99)}},
		{name: "setindexq_existing_oob_nil_preserves", op: tupleRawOp(0x6f73, 16), stack: []any{tupleParityInts(17600, 1), nil}},
		{name: "isnull_nan_false", op: tupleRawOp(0x6e, 8), stack: []any{vm.NaN{}}},
		{name: "qtlen_nan_minus_one", op: tupleRawOp(0x6f89, 16), stack: []any{vm.NaN{}}},
		{name: "indexq_tuple_nan_value", op: tupleRawOp(0x6f60, 16), stack: []any{tuplepkg.NewTupleValue(vm.NaN{}, nil)}},
		{name: "untuple_nan_null", op: tupleRawOp(0x6f22, 16), stack: []any{tuplepkg.NewTupleValue(vm.NaN{}, nil)}},
		{name: "indexq_non_tuple", op: tupleRawOp(0x6f60, 16), stack: []any{int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "setindexq_non_tuple", op: tupleRawOp(0x6f70, 16), stack: []any{int64(1), big.NewInt(2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "index_out_of_range", op: tupleRawOp(0x6f10, 16), stack: []any{tupleParityInts(18000, 0)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "setindex_out_of_range", op: tupleRawOp(0x6f51, 16), stack: []any{tupleParityInts(19000, 1), big.NewInt(3)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "tlen_non_tuple_typecheck", op: tupleRawOp(0x6f88, 16), stack: []any{int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "untuple_wrong_arity", op: tupleRawOp(0x6f23, 16), stack: []any{tupleParityInts(20000, 2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "unpackfirst_too_short", op: tupleRawOp(0x6f33, 16), stack: []any{tupleParityInts(21000, 2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "explode_too_large", op: tupleRawOp(0x6f41, 16), stack: []any{tupleParityInts(22000, 2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "tpush_empty_tuple_value", op: tupleRawOp(0x6f8c, 16), stack: []any{tupleParityInts(22500, 0), big.NewInt(7)}},
		{name: "tpush_len254_to255", op: tupleRawOp(0x6f8c, 16), stack: []any{tupleParityInts(22600, 254), big.NewInt(7)}},
		{name: "tpush_too_large", op: tupleRawOp(0x6f8c, 16), stack: []any{tupleParityInts(23000, 255), big.NewInt(4)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "tpop_singleton_to_empty", op: tupleRawOp(0x6f8d, 16), stack: []any{tupleParityInts(23500, 1)}},
		{name: "tpop_empty", op: tupleRawOp(0x6f8d, 16), stack: []any{tupleParityInts(24000, 0)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "last_empty", op: tupleRawOp(0x6f8b, 16), stack: []any{tupleParityInts(25000, 0)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "index2_outer_oob_range", op: tupleRawOp(0x6fb0, 16), stack: []any{tupleParityInts(25200, 0)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "index2_final_oob_range", op: tupleRawOp(0x6fb1, 16), stack: []any{tuplepkg.NewTupleValue(tupleParityInts(25300, 1))}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "index2_intermediate_non_tuple", op: tupleRawOp(0x6fb0, 16), stack: []any{tuplepkg.NewTupleValue(big.NewInt(1))}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "index2_intermediate_oversized_tuple", op: tupleRawOp(0x6fb0, 16), stack: []any{tuplepkg.NewTupleValue(tupleParityInts(26000, 256))}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "index3_second_level_non_tuple", op: tupleRawOp(0x6fc0, 16), stack: []any{tuplepkg.NewTupleValue(tupleParityInts(26100, 1))}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "index3_final_oob_range", op: tupleRawOp(0x6fc1, 16), stack: []any{tuplepkg.NewTupleValue(tuplepkg.NewTupleValue(tupleParityInts(26200, 1)))}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "nullswapif_non_integer", op: tupleRawOp(0x6fa0, 16), stack: []any{nil}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "indexvar_short_stack_preserves_error", op: tupleRawOp(0x6f81, 16), stack: []any{int64(300)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "untuplevar_short_stack_preserves_error", op: tupleRawOp(0x6f82, 16), stack: []any{int64(3)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "unpackfirstvar_short_stack_preserves_error", op: tupleRawOp(0x6f83, 16), stack: []any{int64(3)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "explodevar_short_stack_preserves_error", op: tupleRawOp(0x6f84, 16), stack: []any{int64(3)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "setindex_short_stack_preserves_error", op: tupleRawOp(0x6f50, 16), stack: []any{int64(7)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "setindexq_short_stack_consumes_value", op: tupleRawOp(0x6f70, 16), stack: []any{int64(7)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "setindexvar_short_stack_preserves_error", op: tupleRawOp(0x6f85, 16), stack: []any{int64(7), int64(0)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "indexvarq_short_stack_nan_idx", op: tupleRawOp(0x6f86, 16), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "setindexvarq_short_stack_nan_idx", op: tupleRawOp(0x6f87, 16), stack: []any{int64(777), vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "tpush_short_stack_preserves_error", op: tupleRawOp(0x6f8c, 16), stack: []any{int64(7)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "tuplevar_256_range", op: tupleRawOp(0x6f80, 16), stack: []any{int64(256)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "indexvar_255_range", op: tupleRawOp(0x6f81, 16), stack: []any{tupleParityInts(27000, 255), int64(255)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "untuplevar_256_range", op: tupleRawOp(0x6f82, 16), stack: []any{tupleParityInts(28000, 0), int64(256)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "unpackfirstvar_256_range", op: tupleRawOp(0x6f83, 16), stack: []any{tupleParityInts(29000, 0), int64(256)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "explodevar_256_range", op: tupleRawOp(0x6f84, 16), stack: []any{tupleParityInts(30000, 0), int64(256)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "setindexvar_255_range", op: tupleRawOp(0x6f85, 16), stack: []any{tupleParityInts(31000, 255), big.NewInt(5), int64(255)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "indexvarq_255_range", op: tupleRawOp(0x6f86, 16), stack: []any{tupleParityInts(32000, 255), int64(255)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "setindexvarq_255_range", op: tupleRawOp(0x6f87, 16), stack: []any{tupleParityInts(33000, 255), big.NewInt(6), int64(255)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "invalid_6f8e_gap", op: tupleRawOp(0x6f8e, 16), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "invalid_6f9f_gap", op: tupleRawOp(0x6f9f, 16), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "invalid_6fa8_gap", op: tupleRawOp(0x6fa8, 16), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "invalid_6faf_gap", op: tupleRawOp(0x6faf, 16), exit: int32(vmerr.CodeInvalidOpcode)},
	}
}

func runTupleOpParityCase(t *testing.T, tc tupleOpParityCase) {
	t.Helper()

	initial, err := buildCrossStack(tc.stack...)
	if err != nil {
		t.Fatalf("failed to build initial stack: %v", err)
	}

	runStackOpParityProgram(t, buildStackProgram(t, []*cell.Builder{tc.op}), initial, tc.exit)
}

func runTupleOpVersionedParityCase(t *testing.T, tc tupleOpParityCase, globalVersion int) {
	t.Helper()

	goStack, err := buildCrossStack(tc.stack...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(tc.stack...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	code := buildStackProgram(t, []*cell.Builder{tc.op})
	goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), tuplepkg.Tuple{}, goStack, globalVersion)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != tc.exit || refRes.exitCode != tc.exit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tc.exit)
	}
	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
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

func tupleRawOp(value uint64, bits uint) *cell.Builder {
	return stackRawOp(value, bits)
}

func tupleParityValues(start, n int) []any {
	values := make([]any, n)
	for i := range values {
		values[i] = big.NewInt(int64(start + i))
	}
	return values
}

func tupleParityInts(start, n int) tuplepkg.Tuple {
	return tuplepkg.NewTupleValue(tupleParityValues(start, n)...)
}

func tupleParityNested2() tuplepkg.Tuple {
	values := make([]any, 4)
	for i := range values {
		values[i] = tupleParityInts(12000+i*10, 4)
	}
	return tuplepkg.NewTupleValue(values...)
}

func tupleParityNested3() tuplepkg.Tuple {
	values := make([]any, 4)
	for i := range values {
		mid := make([]any, 4)
		for j := range mid {
			mid[j] = tupleParityInts(13000+i*100+j*10, 4)
		}
		values[i] = tuplepkg.NewTupleValue(mid...)
	}
	return tuplepkg.NewTupleValue(values...)
}

func tupleNullOpStack(op uint64, x int64) []any {
	if op == 0x6fa2 || op == 0x6fa3 || op == 0x6fa6 || op == 0x6fa7 {
		return []any{big.NewInt(444), big.NewInt(x)}
	}
	return []any{big.NewInt(x)}
}
