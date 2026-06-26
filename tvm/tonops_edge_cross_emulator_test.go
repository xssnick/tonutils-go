//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorTonOpsEdgeCandidates(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	feeC7 := feeTestC7(t)
	extraBalanceC7 := feeExtraBalanceTestC7(t)
	malformedExtraBalanceC7 := malformedExtraBalanceTestC7(t)
	dataSizeLeaf := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	dataSizeRoot := cell.BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(dataSizeLeaf).
		MustStoreRef(dataSizeLeaf).
		EndCell()

	tests := []struct {
		name     string
		code     *cell.Cell
		stack    []any
		exit     int32
		c7       tuple.Tuple
		gasLimit int64
	}{
		{
			name:  "hashext_dynamic_sha256",
			code:  codeFromBuilders(t, funcsop.HASHEXT(255).Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("dynamic"), 56).ToSlice(), int64(1), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name: "hashextr_reverses_two_inputs",
			code: codeFromBuilders(t, funcsop.HASHEXT(1<<8|0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("ab"), 16).ToSlice(),
				cell.BeginCell().MustStoreSlice([]byte("cd"), 16).ToSlice(),
				int64(2),
			},
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "hashexta_appends_sha256_to_builder",
			code: codeFromBuilders(t, funcsop.HASHEXT(1<<9|0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0xAA, 8),
				cell.BeginCell().MustStoreSlice([]byte("append"), 48).ToSlice(),
				int64(1),
			},
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "hashextar_dynamic_builder_input",
			code: codeFromBuilders(t, funcsop.HASHEXT(1<<9|1<<8|255).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0x11, 8),
				cell.BeginCell().MustStoreUInt(0xABCD, 16),
				int64(1),
				int64(0),
			},
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "hashexta_builder_overflow",
			code: codeFromBuilders(t, funcsop.HASHEXT(1<<9|0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 125), 1000),
				cell.BeginCell().MustStoreSlice([]byte("x"), 8).ToSlice(),
				int64(1),
			},
			exit: int32(vmerr.CodeCellOverflow),
			c7:   feeC7,
		},
		{
			name:  "hashext_dynamic_bad_hash_id",
			code:  codeFromBuilders(t, funcsop.HASHEXT(255).Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("x"), 8).ToSlice(), int64(1), int64(255)},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
		{
			name:  "hashext_dynamic_missing_count",
			code:  codeFromBuilders(t, funcsop.HASHEXT(255).Serialize()),
			stack: []any{int64(0)},
			exit:  int32(vmerr.CodeStackUnderflow),
			c7:    feeC7,
		},
		{
			name: "hashbu_builder_with_ref",
			code: codeFromBuilders(t, funcsop.HASHBU().Serialize()),
			stack: []any{
				cell.BeginCell().
					MustStoreUInt(0xCA, 8).
					MustStoreRef(cell.BeginCell().MustStoreUInt(0xFE, 8).EndCell()),
			},
			exit: 0,
			c7:   feeC7,
		},
		{
			name:  "getextrabalance_hit",
			code:  codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
			c7:    extraBalanceC7,
		},
		{
			name:  "getextrabalance_miss",
			code:  codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			stack: []any{int64(8)},
			exit:  0,
			c7:    extraBalanceC7,
		},
		{
			name:  "getextrabalance_nil_dict",
			code:  codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getextrabalance_malformed_value",
			code:  codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			stack: []any{int64(7)},
			exit:  int32(vmerr.CodeCellUnderflow),
			c7:    malformedExtraBalanceC7,
		},
		{
			name: "getextrabalance_repeated_sixth_call_gas",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
				funcsop.GETEXTRABALANCE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
				funcsop.GETEXTRABALANCE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
				funcsop.GETEXTRABALANCE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
				funcsop.GETEXTRABALANCE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
				funcsop.GETEXTRABALANCE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
				funcsop.GETEXTRABALANCE().Serialize(),
			),
			exit: 0,
			c7:   extraBalanceC7,
		},
		{
			name:  "cdatasizeq_limit_too_small",
			code:  codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot, int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "cdatasizeq_short_stack_nan_bound",
			code:  codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  int32(vmerr.CodeStackUnderflow),
			c7:    feeC7,
		},
		{
			name:  "cdatasizeq_nan_bound_is_range_check",
			code:  codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot, vm.NaN{}},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
		{
			name:  "cdatasizeq_wrong_cell_precedes_negative_bound",
			code:  codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack: []any{int64(777), int64(-1)},
			exit:  int32(vmerr.CodeTypeCheck),
			c7:    feeC7,
		},
		{
			name:  "cdatasizeq_huge_bound_saturates",
			code:  codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot, new(big.Int).Lsh(big.NewInt(1), 200)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "cdatasize_null_cell",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{nil, int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "cdatasize_limit_too_small",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{dataSizeRoot, int64(1)},
			exit:  int32(vmerr.CodeCellOverflow),
			c7:    feeC7,
		},
		{
			name:  "cdatasize_short_stack_bad_bound",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{int64(777)},
			exit:  int32(vmerr.CodeStackUnderflow),
			c7:    feeC7,
		},
		{
			name:  "cdatasize_nan_bound_is_range_check",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{dataSizeRoot, vm.NaN{}},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
		{
			name:  "cdatasize_wrong_cell_precedes_negative_bound",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{int64(777), int64(-1)},
			exit:  int32(vmerr.CodeTypeCheck),
			c7:    feeC7,
		},
		{
			name:     "cdatasize_low_gas_load_error",
			code:     codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack:    []any{dataSizeRoot, int64(10)},
			exit:     int32(^vmerr.CodeOutOfGas),
			c7:       feeC7,
			gasLimit: 120,
		},
		{
			name:  "sdatasizeq_limit_zero",
			code:  codeFromBuilders(t, funcsop.SDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot.MustBeginParse(), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "sdatasizeq_short_stack_nan_bound",
			code:  codeFromBuilders(t, funcsop.SDATASIZEQ().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  int32(vmerr.CodeStackUnderflow),
			c7:    feeC7,
		},
		{
			name:  "sdatasizeq_nan_bound_is_range_check",
			code:  codeFromBuilders(t, funcsop.SDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot.MustBeginParse(), vm.NaN{}},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
		{
			name:  "sdatasizeq_wrong_slice_precedes_negative_bound",
			code:  codeFromBuilders(t, funcsop.SDATASIZEQ().Serialize()),
			stack: []any{int64(777), int64(-1)},
			exit:  int32(vmerr.CodeTypeCheck),
			c7:    feeC7,
		},
		{
			name:  "sdatasize_limit_zero",
			code:  codeFromBuilders(t, funcsop.SDATASIZE().Serialize()),
			stack: []any{dataSizeRoot.MustBeginParse(), int64(0)},
			exit:  int32(vmerr.CodeCellOverflow),
			c7:    feeC7,
		},
		{
			name:  "sdatasize_nan_bound_is_range_check",
			code:  codeFromBuilders(t, funcsop.SDATASIZE().Serialize()),
			stack: []any{dataSizeRoot.MustBeginParse(), vm.NaN{}},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
		{
			name:  "sdatasize_wrong_slice_precedes_negative_bound",
			code:  codeFromBuilders(t, funcsop.SDATASIZE().Serialize()),
			stack: []any{int64(777), int64(-1)},
			exit:  int32(vmerr.CodeTypeCheck),
			c7:    feeC7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runTonOpsEdgeParityCase(t, prependRawMethodDrop(tt.code), tt.stack, tt.c7, tt.exit, tt.gasLimit)
		})
	}
}

type tonOpsEdgeVersionCase struct {
	name     string
	code     *cell.Cell
	stack    []any
	exit     func(int) int32
	c7       tuple.Tuple
	gasLimit int64
}

func TestTVMCrossEmulatorTonOpsEdgeAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := tonOpsEdgeVersionCases(t)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_TONOPS_EDGE_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runTonOpsEdgeVersionedParityCase(t, tt.code, tt.stack, tt.c7, version, tt.exit(version), tt.gasLimit)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTonOpsEdgeGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%tonOpsEdgeVersionCaseCount))
	}
	for i := 0; i < tonOpsEdgeVersionCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := tonOpsEdgeVersionCases(t)
		if len(tests) != tonOpsEdgeVersionCaseCount {
			t.Fatalf("tonops edge case count = %d, want %d", len(tests), tonOpsEdgeVersionCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runTonOpsEdgeVersionedParityCase(t, tt.code, tt.stack, tt.c7, version, tt.exit(version), tt.gasLimit)
	})
}

const tonOpsEdgeVersionCaseCount = 26

func tonOpsEdgeVersionCases(t *testing.T) []tonOpsEdgeVersionCase {
	t.Helper()

	feeC7 := feeTestC7(t)
	dataSizeLeaf := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	dataSizeRoot := cell.BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(dataSizeLeaf).
		MustStoreRef(dataSizeLeaf).
		EndCell()

	versionedExit := func(version, min int, exit int32) int32 {
		if version < min {
			return int32(vmerr.CodeInvalidOpcode)
		}
		return exit
	}
	hashExtExit := func(exit int32) func(int) int32 {
		return func(version int) int32 {
			return versionedExit(version, 4, exit)
		}
	}
	dataSizeExit := func(exit int32) func(int) int32 {
		return func(int) int32 {
			return exit
		}
	}

	tests := []tonOpsEdgeVersionCase{
		{
			name:  "hashext_dynamic_sha256",
			code:  codeFromBuilders(t, funcsop.HASHEXT(255).Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("dynamic"), 56).ToSlice(), int64(1), int64(0)},
			exit:  hashExtExit(0),
			c7:    feeC7,
		},
		{
			name: "hashextr_reverses_two_inputs",
			code: codeFromBuilders(t, funcsop.HASHEXT(1<<8|0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("ab"), 16).ToSlice(),
				cell.BeginCell().MustStoreSlice([]byte("cd"), 16).ToSlice(),
				int64(2),
			},
			exit: hashExtExit(0),
			c7:   feeC7,
		},
		{
			name: "hashexta_appends_sha256_to_builder",
			code: codeFromBuilders(t, funcsop.HASHEXT(1<<9|0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0xAA, 8),
				cell.BeginCell().MustStoreSlice([]byte("append"), 48).ToSlice(),
				int64(1),
			},
			exit: hashExtExit(0),
			c7:   feeC7,
		},
		{
			name: "hashextar_dynamic_builder_input",
			code: codeFromBuilders(t, funcsop.HASHEXT(1<<9|1<<8|255).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0x11, 8),
				cell.BeginCell().MustStoreUInt(0xABCD, 16),
				int64(1),
				int64(0),
			},
			exit: hashExtExit(0),
			c7:   feeC7,
		},
		{
			name:  "hashext_dynamic_bad_hash_id",
			code:  codeFromBuilders(t, funcsop.HASHEXT(255).Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("x"), 8).ToSlice(), int64(1), int64(255)},
			exit:  hashExtExit(int32(vmerr.CodeRangeCheck)),
			c7:    feeC7,
		},
		{
			name:  "hashext_dynamic_missing_count",
			code:  codeFromBuilders(t, funcsop.HASHEXT(255).Serialize()),
			stack: []any{int64(0)},
			exit:  hashExtExit(int32(vmerr.CodeStackUnderflow)),
			c7:    feeC7,
		},
		{
			name: "hashexta_builder_overflow",
			code: codeFromBuilders(t, funcsop.HASHEXT(1<<9|0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 125), 1000),
				cell.BeginCell().MustStoreSlice([]byte("x"), 8).ToSlice(),
				int64(1),
			},
			exit: hashExtExit(int32(vmerr.CodeCellOverflow)),
			c7:   feeC7,
		},
		{
			name: "hashbu_builder_with_ref",
			code: codeFromBuilders(t, funcsop.HASHBU().Serialize()),
			stack: []any{
				cell.BeginCell().
					MustStoreUInt(0xCA, 8).
					MustStoreRef(cell.BeginCell().MustStoreUInt(0xFE, 8).EndCell()),
			},
			exit: func(version int) int32 {
				return versionedExit(version, 12, 0)
			},
			c7: feeC7,
		},
		{
			name:  "cdatasizeq_limit_too_small",
			code:  codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot, int64(1)},
			exit:  dataSizeExit(0),
			c7:    feeC7,
		},
		{
			name:  "cdatasizeq_short_stack_nan_bound",
			code:  codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  dataSizeExit(int32(vmerr.CodeStackUnderflow)),
			c7:    feeC7,
		},
		{
			name:  "cdatasizeq_nan_bound_is_range_check",
			code:  codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot, vm.NaN{}},
			exit:  dataSizeExit(int32(vmerr.CodeRangeCheck)),
			c7:    feeC7,
		},
		{
			name:  "cdatasizeq_wrong_cell_precedes_negative_bound",
			code:  codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack: []any{int64(777), int64(-1)},
			exit:  dataSizeExit(int32(vmerr.CodeTypeCheck)),
			c7:    feeC7,
		},
		{
			name:  "cdatasizeq_huge_bound_saturates",
			code:  codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot, new(big.Int).Lsh(big.NewInt(1), 200)},
			exit:  dataSizeExit(0),
			c7:    feeC7,
		},
		{
			name:  "cdatasize_null_cell",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{nil, int64(1)},
			exit:  dataSizeExit(0),
			c7:    feeC7,
		},
		{
			name:  "cdatasize_limit_too_small",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{dataSizeRoot, int64(1)},
			exit:  dataSizeExit(int32(vmerr.CodeCellOverflow)),
			c7:    feeC7,
		},
		{
			name:  "cdatasize_short_stack_bad_bound",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{int64(777)},
			exit:  dataSizeExit(int32(vmerr.CodeStackUnderflow)),
			c7:    feeC7,
		},
		{
			name:  "cdatasize_nan_bound_is_range_check",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{dataSizeRoot, vm.NaN{}},
			exit:  dataSizeExit(int32(vmerr.CodeRangeCheck)),
			c7:    feeC7,
		},
		{
			name:  "cdatasize_wrong_cell_precedes_negative_bound",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{int64(777), int64(-1)},
			exit:  dataSizeExit(int32(vmerr.CodeTypeCheck)),
			c7:    feeC7,
		},
		{
			name:     "cdatasize_low_gas_load_error",
			code:     codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack:    []any{dataSizeRoot, int64(10)},
			exit:     dataSizeExit(int32(^vmerr.CodeOutOfGas)),
			c7:       feeC7,
			gasLimit: 120,
		},
		{
			name:  "sdatasizeq_limit_zero",
			code:  codeFromBuilders(t, funcsop.SDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot.MustBeginParse(), int64(0)},
			exit:  dataSizeExit(0),
			c7:    feeC7,
		},
		{
			name:  "sdatasizeq_short_stack_nan_bound",
			code:  codeFromBuilders(t, funcsop.SDATASIZEQ().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  dataSizeExit(int32(vmerr.CodeStackUnderflow)),
			c7:    feeC7,
		},
		{
			name:  "sdatasizeq_nan_bound_is_range_check",
			code:  codeFromBuilders(t, funcsop.SDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot.MustBeginParse(), vm.NaN{}},
			exit:  dataSizeExit(int32(vmerr.CodeRangeCheck)),
			c7:    feeC7,
		},
		{
			name:  "sdatasizeq_wrong_slice_precedes_negative_bound",
			code:  codeFromBuilders(t, funcsop.SDATASIZEQ().Serialize()),
			stack: []any{int64(777), int64(-1)},
			exit:  dataSizeExit(int32(vmerr.CodeTypeCheck)),
			c7:    feeC7,
		},
		{
			name:  "sdatasize_limit_zero",
			code:  codeFromBuilders(t, funcsop.SDATASIZE().Serialize()),
			stack: []any{dataSizeRoot.MustBeginParse(), int64(0)},
			exit:  dataSizeExit(int32(vmerr.CodeCellOverflow)),
			c7:    feeC7,
		},
		{
			name:  "sdatasize_nan_bound_is_range_check",
			code:  codeFromBuilders(t, funcsop.SDATASIZE().Serialize()),
			stack: []any{dataSizeRoot.MustBeginParse(), vm.NaN{}},
			exit:  dataSizeExit(int32(vmerr.CodeRangeCheck)),
			c7:    feeC7,
		},
		{
			name:  "sdatasize_wrong_slice_precedes_negative_bound",
			code:  codeFromBuilders(t, funcsop.SDATASIZE().Serialize()),
			stack: []any{int64(777), int64(-1)},
			exit:  dataSizeExit(int32(vmerr.CodeTypeCheck)),
			c7:    feeC7,
		},
	}

	return tests
}

func runTonOpsEdgeParityCase(t *testing.T, code *cell.Cell, stackValues []any, c7 tuple.Tuple, exit int32, gasLimit int64) {
	t.Helper()

	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	if gasLimit == 0 {
		gasLimit = referenceDefaultMaxGas
	}

	goRes, err := runGoCrossCodeWithGas(code, cell.BeginCell().EndCell(), c7, goStack, gasLimit)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCodeWithGas(code, cell.BeginCell().EndCell(), c7, refStack, gasLimit)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != exit || refRes.exitCode != exit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, exit)
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

func runTonOpsEdgeVersionedParityCase(t *testing.T, code *cell.Cell, stackValues []any, c7 tuple.Tuple, globalVersion int, exit int32, gasLimit int64) {
	t.Helper()

	code = prependRawMethodDrop(code)
	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	if gasLimit == 0 {
		gasLimit = referenceDefaultMaxGas
	}

	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, cell.BeginCell().EndCell(), c7, nil, goStack, globalVersion, gasLimit)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refCfg := *tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refCfg.GasLimit = gasLimit
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != exit || refRes.exitCode != exit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, exit)
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

func malformedExtraBalanceTestC7(t *testing.T) tuple.Tuple {
	t.Helper()

	extraDict := cell.NewDict(32)
	if _, err := extraDict.SetBuilderWithMode(
		cell.BeginCell().MustStoreUInt(7, 32).EndCell(),
		cell.BeginCell().MustStoreUInt(0xF, 4),
		cell.DictSetModeSet,
	); err != nil {
		t.Fatalf("failed to seed malformed extra balance dict: %v", err)
	}

	return makeTonopsTestC7(t, tonopsTestC7Config{
		Balance: tuple.NewTupleValue(new(big.Int).Set(tonopsTestBalance), extraDict.AsCell()),
	})
}
