//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const contOpsParityCaseCount = 213

type contOpsParityCase struct {
	name          string
	code          *cell.Cell
	stack         []any
	exit          int32
	skipReference func(int) string
	goStack       []any
}

func contOpsDuplicateSaveReferenceSkip(version int) string {
	if version >= 14 {
		return "bundled reference emulator predates upstream control-register v14 silent duplicate save-list writes"
	}
	return ""
}

func TestTVMCrossEmulatorContOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := contOpsParityCases(t)
	if len(tests) != contOpsParityCaseCount {
		t.Fatalf("contops parity case count = %d, want %d", len(tests), contOpsParityCaseCount)
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runContParityCase(t, tt)
		})
	}
}

func TestTVMCrossEmulatorContOpsAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := contOpsParityCases(t)
	if len(tests) != contOpsParityCaseCount {
		t.Fatalf("contops parity case count = %d, want %d", len(tests), contOpsParityCaseCount)
	}
	versions := crossEmulatorVersionAuditVersions(t, "TVM_CONTOPS_CORE_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runContCoreVersionedParityCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorContOpsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(version%contOpsParityCaseCount))
	}
	for i := 0; i < contOpsParityCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint16(i))
	}
	f.Add(uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := contOpsParityCases(t)
		if len(tests) != contOpsParityCaseCount {
			t.Fatalf("contops parity case count = %d, want %d", len(tests), contOpsParityCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runContCoreVersionedParityCase(t, tt, version)
	})
}

func runContCoreVersionedParityCase(t *testing.T, tt contOpsParityCase, version int) {
	t.Helper()

	skipReference := ""
	if tt.skipReference != nil {
		skipReference = tt.skipReference(version)
	}
	runContVersionedParityCaseWithExpected(t, tt.code, tt.stack, version, nil, skipReference, tt.goStack)
}

func contOpsParityCases(t *testing.T) []contOpsParityCase {
	t.Helper()

	intBody := func(v int64) *cell.Cell {
		return codeFromBuilders(t, stackop.PUSHINT(big.NewInt(v)).Serialize())
	}

	retAltBody := codeFromBuilders(t, execop.RETALT().Serialize())
	retBody := codeFromBuilders(t, execop.RET().Serialize())
	dropThen7Body := codeFromBuilders(t, stackop.DROP().Serialize(), stackop.PUSHINT(big.NewInt(7)).Serialize())
	emptyBody := codeFromBuilders(t)
	remainingCodeBody := codeFromBuilders(t, execop.RETDATA().Serialize(), stackop.PUSHINT(big.NewInt(3)).Serialize())
	returnOverflowBody := codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(1)).Serialize(),
		execop.RETURNARGS(0).Serialize(),
		execop.RET().Serialize(),
	)
	push7Body := intBody(7)
	push7Slice := push7Body.MustBeginParse()
	unresolvedLibraryRef, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(make([]byte, 32), 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to build unresolved library ref: %v", err)
	}

	return []contOpsParityCase{
		{
			name: "ret_via_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, execop.RET().Serialize())).Serialize(),
				execop.EXECUTE().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			exit: 0,
		},
		{
			name:  "ifret_true_exit0",
			code:  codeFromBuilders(t, execop.IFRET().Serialize(), stackop.PUSHINT(big.NewInt(9)).Serialize()),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name:  "ifret_false_continue",
			code:  codeFromBuilders(t, execop.IFRET().Serialize(), stackop.PUSHINT(big.NewInt(9)).Serialize()),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "ifret_non_int_condition_typecheck",
			code:  codeFromBuilders(t, execop.IFRET().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifretalt_true_exit1",
			code:  codeFromBuilders(t, execop.IFRETALT().Serialize(), stackop.PUSHINT(big.NewInt(9)).Serialize()),
			stack: []any{int64(-1)},
			exit:  1,
		},
		{
			name:  "ifretalt_non_int_condition_typecheck",
			code:  codeFromBuilders(t, execop.IFRETALT().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifnotretalt_false_exit1",
			code:  codeFromBuilders(t, execop.IFNOTRETALT().Serialize()),
			stack: []any{int64(0)},
			exit:  1,
		},
		{
			name:  "ifnotretalt_non_int_condition_typecheck",
			code:  codeFromBuilders(t, execop.IFNOTRETALT().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifnotret_true_continues_with_residual",
			code:  codeFromBuilders(t, execop.IFNOTRET().Serialize(), stackop.PUSHINT(big.NewInt(9)).Serialize()),
			stack: []any{int64(11), int64(-1)},
			exit:  0,
		},
		{
			name:  "ifnotret_non_int_condition_typecheck",
			code:  codeFromBuilders(t, execop.IFNOTRET().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "retbool_false_exit1",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, execop.RETBOOL().Serialize())).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(0)},
			exit:  1,
		},
		{
			name: "retbool_non_int_condition_typecheck",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, execop.RETBOOL().Serialize())).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "retdata_pushes_remaining_code",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(remainingCodeBody).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "retargs_trim",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, execop.RETARGS(1).Serialize())).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(11), int64(22)},
			exit:  0,
		},
		{
			name: "callxargs_trim",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.CALLXARGS(2, 1).Serialize(),
			),
			stack: []any{int64(11), int64(22)},
			exit:  0,
		},
		{
			name: "callxargs_p_all",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.CALLXARGSP(1).Serialize(),
			),
			stack: []any{int64(55)},
			exit:  0,
		},
		{
			name: "callcc_pushes_cc",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				execop.CALLCC().Serialize(),
			),
			exit: 0,
		},
		{
			name: "callccargs_preserves_arg",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				execop.CALLCCARGS(1, 2).Serialize(),
			),
			stack: []any{int64(55)},
			exit:  0,
		},
		{
			name: "callxvarargs_dynamic",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.XCHG0(2).Serialize(),
				stackop.XCHG0(1).Serialize(),
				execop.CALLXVARARGS().Serialize(),
			),
			stack: []any{int64(11), int64(22), int64(2), int64(1)},
			exit:  0,
		},
		{
			name: "callxvarargs_pass_all_return_keeps_single_copy",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retBody).Serialize(),
				stackop.PUSHINT(big.NewInt(-1)).Serialize(),
				stackop.PUSHINT(big.NewInt(-1)).Serialize(),
				execop.CALLXVARARGS().Serialize(),
			),
			stack: []any{int64(11), int64(22)},
			exit:  0,
		},
		{
			name:  "retvarargs_trim",
			code:  codeFromBuilders(t, execop.RETVARARGS().Serialize()),
			stack: []any{int64(11), int64(22), int64(1)},
			exit:  0,
		},
		{
			name: "returnvarargs_bad_count_range_consumes_count",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(256)).Serialize(),
				execop.RETURNVARARGS().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "returnvarargs_short_count_consumes_count",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
				execop.RETURNVARARGS().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "returnargs_short_fixed_count_preserves_stack",
			code:  codeFromBuilders(t, execop.RETURNARGS(2).Serialize()),
			stack: []any{int64(11)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "returnargs_exact_count_noop",
			code:  codeFromBuilders(t, execop.RETURNARGS(2).Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  0,
		},
		{
			name: "returnargs_existing_closure_stack_execute",
			code: codeFromBuilders(t,
				execop.BLESSARGS(1, -1).Serialize(),
				execop.POPCTR(0).Serialize(),
				stackop.PUSHINT(big.NewInt(33)).Serialize(),
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
				execop.RETURNARGS(1).Serialize(),
				execop.RET().Serialize(),
			),
			stack: []any{int64(11), int64(22), push7Body.MustBeginParse()},
			exit:  0,
		},
		{
			name: "returnargs_closure_overflow_keeps_return_values_stack",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(returnOverflowBody).Serialize(),
				execop.CALLXARGS(0, 0).Serialize(),
			),
			exit: vmerr.CodeStackOverflow,
		},
		{
			name: "callccvarargs_dynamic",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				stackop.XCHG0(2).Serialize(),
				stackop.XCHG0(1).Serialize(),
				execop.CALLCCVARARGS().Serialize(),
			),
			stack: []any{int64(55), int64(1), int64(2)},
			exit:  0,
		},
		{
			name: "callccvarargs_bad_retvals_range_consumes_retvals",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				stackop.PUSHINT(big.NewInt(255)).Serialize(),
				execop.CALLCCVARARGS().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "callccvarargs_bad_params_range_consumes_counts",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				stackop.PUSHINT(big.NewInt(255)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.CALLCCVARARGS().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "callccvarargs_short_dynamic_args_keeps_cont",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.CALLCCVARARGS().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "ifbitjmp_true",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				execop.IFBITJMP(1).Serialize(),
			),
			stack: []any{int64(2)},
			exit:  0,
		},
		{
			name: "ifnbitjmp_true",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(dropThen7Body).Serialize(),
				execop.IFNBITJMP(1).Serialize(),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "ifbitjmpref_true",
			code:  codeFromBuilders(t, execop.IFBITJMPREF(1, dropThen7Body).Serialize()),
			stack: []any{int64(2)},
			exit:  0,
		},
		{
			name: "callref",
			code: codeFromBuilders(t, execop.CALLREF(intBody(7)).Serialize()),
			exit: 0,
		},
		{
			name:  "callref_unresolved_library_ref_preserves_stack",
			code:  codeFromBuilders(t, execop.CALLREF(unresolvedLibraryRef).Serialize()),
			stack: []any{int64(11)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name: "jmpref",
			code: codeFromBuilders(t, execop.JMPREF(intBody(8)).Serialize()),
			exit: 0,
		},
		{
			name:  "jmpref_unresolved_library_ref_preserves_stack",
			code:  codeFromBuilders(t, execop.JMPREF(unresolvedLibraryRef).Serialize()),
			stack: []any{int64(11)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name: "jmprefdata",
			code: codeFromBuilders(t, execop.JMPREFDATA(dropThen7Body).Serialize()),
			exit: 0,
		},
		{
			name:  "jmprefdata_unresolved_library_ref_leaves_current_code",
			code:  codeFromBuilders(t, execop.JMPREFDATA(unresolvedLibraryRef).Serialize()),
			stack: []any{int64(11)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name:  "jmprefdata_missing_ref_invalid_preserves_stack",
			code:  codeFromOpcodes(t, 0xDB3E),
			stack: []any{int64(11)},
			exit:  vmerr.CodeInvalidOpcode,
		},
		{
			name:  "ifref_true",
			code:  codeFromBuilders(t, execop.IFREF(intBody(7)).Serialize()),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name:  "ifref_false_skips_ref_with_residual",
			code:  codeFromBuilders(t, execop.IFREF(intBody(7)).Serialize()),
			stack: []any{int64(11), int64(0)},
			exit:  0,
		},
		{
			name:  "ifref_missing_ref_invalid_preserves_condition",
			code:  codeFromOpcodes(t, 0xE300),
			stack: []any{int64(0)},
			exit:  vmerr.CodeInvalidOpcode,
		},
		{
			name:  "ifref_non_int_condition_typecheck",
			code:  codeFromBuilders(t, execop.IFREF(intBody(7)).Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifref_taken_unresolved_library_ref_consumes_condition",
			code:  codeFromBuilders(t, execop.IFREF(unresolvedLibraryRef).Serialize()),
			stack: []any{int64(11), int64(-1)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name:  "ifnotref_false",
			code:  codeFromBuilders(t, execop.IFNOTREF(intBody(7)).Serialize()),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "ifnotref_true_skips_ref_with_residual",
			code:  codeFromBuilders(t, execop.IFNOTREF(intBody(7)).Serialize()),
			stack: []any{int64(11), int64(-1)},
			exit:  0,
		},
		{
			name:  "ifnotref_non_int_condition_typecheck",
			code:  codeFromBuilders(t, execop.IFNOTREF(intBody(7)).Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifnotref_taken_unresolved_library_ref_consumes_condition",
			code:  codeFromBuilders(t, execop.IFNOTREF(unresolvedLibraryRef).Serialize()),
			stack: []any{int64(11), int64(0)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name:  "ifjmpref_true",
			code:  codeFromBuilders(t, execop.IFJMPREF(intBody(7)).Serialize()),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name:  "ifjmpref_false_skips_ref_with_residual",
			code:  codeFromBuilders(t, execop.IFJMPREF(intBody(7)).Serialize()),
			stack: []any{int64(11), int64(0)},
			exit:  0,
		},
		{
			name:  "ifjmpref_non_int_condition_typecheck",
			code:  codeFromBuilders(t, execop.IFJMPREF(intBody(7)).Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifjmpref_taken_unresolved_library_ref_consumes_condition",
			code:  codeFromBuilders(t, execop.IFJMPREF(unresolvedLibraryRef).Serialize()),
			stack: []any{int64(11), int64(-1)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name:  "ifnotjmpref_false",
			code:  codeFromBuilders(t, execop.IFNOTJMPREF(intBody(7)).Serialize()),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "ifnotjmpref_true_skips_ref_with_residual",
			code:  codeFromBuilders(t, execop.IFNOTJMPREF(intBody(7)).Serialize()),
			stack: []any{int64(11), int64(-1)},
			exit:  0,
		},
		{
			name:  "ifnotjmpref_non_int_condition_typecheck",
			code:  codeFromBuilders(t, execop.IFNOTJMPREF(intBody(7)).Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifnotjmpref_taken_unresolved_library_ref_consumes_condition",
			code:  codeFromBuilders(t, execop.IFNOTJMPREF(unresolvedLibraryRef).Serialize()),
			stack: []any{int64(11), int64(0)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name: "ifrefelse_false",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(intBody(9)).Serialize(),
				execop.IFREFELSE(intBody(7)).Serialize(),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name: "ifrefelse_missing_ref_invalid_preserves_stack",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(intBody(9)).Serialize(),
				cell.BeginCell().MustStoreUInt(0xE30D, 16),
			),
			stack: []any{int64(0)},
			exit:  vmerr.CodeInvalidOpcode,
		},
		{
			name:  "ifrefelse_short_stack_preserves_condition",
			code:  codeFromBuilders(t, execop.IFREFELSE(intBody(7)).Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "ifrefelse_bad_continuation_consumes_top_only",
			code:  codeFromBuilders(t, execop.IFREFELSE(intBody(7)).Serialize()),
			stack: []any{int64(-1), int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifrefelse_bad_condition_consumes_continuation_and_condition",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(intBody(9)).Serialize(),
				execop.IFREFELSE(intBody(7)).Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifrefelse_true_unresolved_library_ref_consumes_operands",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(intBody(9)).Serialize(),
				execop.IFREFELSE(unresolvedLibraryRef).Serialize(),
			),
			stack: []any{int64(11), int64(-1)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name: "ifelseref_false",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(intBody(7)).Serialize(),
				execop.IFELSEREF(intBody(9)).Serialize(),
			),
			stack: []any{int64(0)},
			exit:  0,
		},
		{
			name:  "ifelseref_short_stack_preserves_condition",
			code:  codeFromBuilders(t, execop.IFELSEREF(intBody(9)).Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "ifelseref_bad_continuation_consumes_top_only",
			code:  codeFromBuilders(t, execop.IFELSEREF(intBody(9)).Serialize()),
			stack: []any{int64(0), int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifelseref_bad_condition_consumes_continuation_and_condition",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(intBody(7)).Serialize(),
				execop.IFELSEREF(intBody(9)).Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifelseref_false_unresolved_library_ref_consumes_operands",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(intBody(7)).Serialize(),
				execop.IFELSEREF(unresolvedLibraryRef).Serialize(),
			),
			stack: []any{int64(11), int64(0)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name:  "ifrefelseref_true",
			code:  codeFromBuilders(t, execop.IFREFELSEREF(intBody(7), intBody(9)).Serialize()),
			stack: []any{int64(-1)},
			exit:  0,
		},
		{
			name:  "ifrefelseref_non_int_condition_typecheck",
			code:  codeFromBuilders(t, execop.IFREFELSEREF(intBody(7), intBody(9)).Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifrefelseref_missing_second_ref_invalid_preserves_condition",
			code: codeFromBuilders(t,
				cell.BeginCell().
					MustStoreUInt(0xE30F, 16).
					MustStoreRef(intBody(7)),
			),
			stack: []any{int64(-1)},
			exit:  vmerr.CodeInvalidOpcode,
		},
		{
			name:  "ifrefelseref_false_unresolved_library_ref_consumes_condition",
			code:  codeFromBuilders(t, execop.IFREFELSEREF(intBody(7), unresolvedLibraryRef).Serialize()),
			stack: []any{int64(11), int64(0)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name: "setcontargs_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETCONTARGS(1, 0).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(55)},
			exit:  0,
		},
		{
			name: "setcontargs_existing_closure_stack_execute",
			code: codeFromBuilders(t,
				execop.BLESSARGS(1, -1).Serialize(),
				execop.SETCONTARGS(1, -1).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(11), int64(22), push7Body.MustBeginParse()},
			exit:  0,
		},
		{
			name: "setcontvarargs_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.SETCONTVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(66)},
			exit:  0,
		},
		{
			name: "setnumvarargs_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				execop.SETNUMVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(77)},
			exit:  0,
		},
		{
			name: "setnumvarargs_minus_one_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(-1)).Serialize(),
				execop.SETNUMVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(77)},
			exit:  0,
		},
		{
			name:  "bless_execute",
			code:  codeFromBuilders(t, execop.BLESS().Serialize(), execop.EXECUTE().Serialize()),
			stack: []any{push7Slice},
			exit:  0,
		},
		{
			name:  "blessargs_execute",
			code:  codeFromBuilders(t, execop.BLESSARGS(1, 0).Serialize(), execop.EXECUTE().Serialize()),
			stack: []any{int64(55), push7Body.MustBeginParse()},
			exit:  0,
		},
		{
			name:  "blessargs_bad_code_type_consumes_code_only",
			code:  codeFromBuilders(t, execop.BLESSARGS(0, 0).Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "blessvarargs_execute",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.BLESSVARARGS().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{int64(66), push7Body.MustBeginParse()},
			exit:  0,
		},
		{
			name: "if_non_int_condition_typecheck",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.IF().Serialize(),
			),
			stack: []any{cell.BeginCell().EndCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifelse_second_branch_not_continuation",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.IFELSE().Serialize(),
			),
			stack: []any{int64(-1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifelse_top_branch_not_continuation",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				execop.IFELSE().Serialize(),
			),
			stack: []any{int64(-1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "ifelse_non_int_condition_typecheck",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.IFELSE().Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "if_short_stack_underflow_before_type",
			code:  codeFromBuilders(t, execop.IF().Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "if_bad_continuation_consumes_top_only",
			code:  codeFromBuilders(t, execop.IF().Serialize()),
			stack: []any{int64(-1), int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifnot_short_stack_underflow_before_type",
			code:  codeFromBuilders(t, execop.IFNOT().Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "ifnot_non_int_condition_typecheck",
			code:  codeFromBuilders(t, stackop.PUSHCONT(emptyBody).Serialize(), execop.IFNOT().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifnot_bad_continuation_consumes_top_only",
			code:  codeFromBuilders(t, execop.IFNOT().Serialize()),
			stack: []any{int64(0), int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifjmp_short_stack_underflow_before_type",
			code:  codeFromBuilders(t, execop.IFJMP().Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "ifjmp_non_int_condition_typecheck",
			code:  codeFromBuilders(t, stackop.PUSHCONT(emptyBody).Serialize(), execop.IFJMP().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifjmp_bad_continuation_consumes_top_only",
			code:  codeFromBuilders(t, execop.IFJMP().Serialize()),
			stack: []any{int64(-1), int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifnotjmp_short_stack_underflow_before_type",
			code:  codeFromBuilders(t, execop.IFNOTJMP().Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "ifnotjmp_non_int_condition_typecheck",
			code:  codeFromBuilders(t, stackop.PUSHCONT(emptyBody).Serialize(), execop.IFNOTJMP().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifnotjmp_bad_continuation_consumes_top_only",
			code:  codeFromBuilders(t, execop.IFNOTJMP().Serialize()),
			stack: []any{int64(0), int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "ifelse_short_stack_underflow_before_type",
			code:  codeFromBuilders(t, execop.IFELSE().Serialize()),
			stack: []any{int64(0), int64(1)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "jmpx_non_continuation_typecheck",
			code:  codeFromBuilders(t, execop.JMPX().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "execute_non_continuation_typecheck",
			code:  codeFromBuilders(t, execop.EXECUTE().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "jmpxdata_non_continuation_typecheck",
			code:  codeFromBuilders(t, execop.JMPXDATA().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "callxargs_bad_continuation_consumes_top",
			code:  codeFromBuilders(t, execop.CALLXARGS(0, 0).Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "callxargsp_bad_continuation_consumes_top",
			code:  codeFromBuilders(t, execop.CALLXARGSP(0).Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "jmpxargs_bad_continuation_consumes_top",
			code:  codeFromBuilders(t, execop.JMPXARGS(0).Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "jmpxvarargs_underflow",
			code:  codeFromBuilders(t, execop.JMPXVARARGS().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "jmpxvarargs_bad_params_range_consumes_param",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				stackop.PUSHINT(big.NewInt(255)).Serialize(),
				execop.JMPXVARARGS().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "jmpxvarargs_short_dynamic_args_consumes_param",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
				execop.JMPXVARARGS().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "jmpxvarargs_bad_continuation_consumes_param_and_value",
			code:  codeFromBuilders(t, execop.JMPXVARARGS().Serialize()),
			stack: []any{testEmptyCell(), int64(0)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "callccargs_underflow",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.CALLCCARGS(2, 1).Serialize(),
			),
			stack: []any{int64(55)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "callcc_bad_continuation_consumes_top_only",
			code:  codeFromBuilders(t, execop.CALLCC().Serialize()),
			stack: []any{int64(11), testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "callccargs_bad_continuation_consumes_top_only",
			code:  codeFromBuilders(t, execop.CALLCCARGS(1, 0).Serialize()),
			stack: []any{int64(11), testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "callxvarargs_short_stack_preserves_values",
			code:  codeFromBuilders(t, execop.CALLXVARARGS().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "callxvarargs_bad_params_range",
			code:  codeFromBuilders(t, execop.CALLXVARARGS().Serialize()),
			stack: []any{push7Slice, int64(255), int64(0)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "callxvarargs_bad_retvals_range_consumes_retvals",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				stackop.PUSHINT(big.NewInt(255)).Serialize(),
				execop.CALLXVARARGS().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "callxvarargs_short_dynamic_args_consumes_cont_and_counts",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.CALLXVARARGS().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "callxvarargs_bad_continuation_consumes_counts_and_bad_continuation",
			code:  codeFromBuilders(t, execop.CALLXVARARGS().Serialize()),
			stack: []any{int64(11), testEmptyCell(), int64(0), int64(0)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "callccvarargs_short_stack_preserves_values",
			code:  codeFromBuilders(t, execop.CALLCCVARARGS().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "callccvarargs_bad_continuation_consumes_counts_and_bad_continuation",
			code:  codeFromBuilders(t, execop.CALLCCVARARGS().Serialize()),
			stack: []any{int64(11), testEmptyCell(), int64(0), int64(0)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "setcontargs_closure_overflow",
			code: codeFromBuilders(t,
				execop.BLESSARGS(0, 0).Serialize(),
				execop.SETCONTARGS(1, -1).Serialize(),
			),
			stack: []any{int64(55), push7Slice},
			exit:  vmerr.CodeStackOverflow,
		},
		{
			name: "setnumvarargs_bad_range",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(256)).Serialize(),
				execop.SETNUMVARARGS().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name:  "setcontvarargs_short_invalid_more_underflow",
			code:  codeFromBuilders(t, execop.SETCONTVARARGS().Serialize()),
			stack: []any{int64(300)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "setcontvarargs_bad_more_range_consumes_more",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				stackop.PUSHINT(big.NewInt(256)).Serialize(),
				execop.SETCONTVARARGS().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "setcontvarargs_bad_copy_range_consumes_counts",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(256)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.SETCONTVARARGS().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "setcontvarargs_short_dynamic_args_keeps_cont",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.SETCONTVARARGS().Serialize(),
			),
			exit: vmerr.CodeStackUnderflow,
		},
		{
			name:  "setnumvarargs_short_invalid_more_underflow",
			code:  codeFromBuilders(t, execop.SETNUMVARARGS().Serialize()),
			stack: []any{int64(300)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "setnumvarargs_bad_continuation_consumes_more_and_value",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.SETNUMVARARGS().Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "bless_bad_code_type",
			code:  codeFromBuilders(t, execop.BLESS().Serialize()),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "blessvarargs_short_invalid_more_underflow",
			code:  codeFromBuilders(t, execop.BLESSVARARGS().Serialize()),
			stack: []any{int64(300)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "blessvarargs_bad_more_range_consumes_more",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				stackop.PUSHINT(big.NewInt(256)).Serialize(),
				execop.BLESSVARARGS().Serialize(),
			),
			stack: []any{push7Body.MustBeginParse()},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "blessvarargs_bad_copy_range_consumes_counts",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(256)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.BLESSVARARGS().Serialize(),
			),
			stack: []any{push7Body.MustBeginParse()},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name: "blessvarargs_short_dynamic_args_keeps_code",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHINT(big.NewInt(0)).Serialize(),
				execop.BLESSVARARGS().Serialize(),
			),
			stack: []any{push7Body.MustBeginParse()},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "again_exit_alt",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.AGAIN().Serialize(),
			),
			exit: 1,
		},
		{
			name:  "again_bad_body_consumes_top_only",
			code:  codeFromBuilders(t, execop.AGAIN().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "againend_exit_alt",
			code: codeFromBuilders(t,
				execop.AGAINEND().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 1,
		},
		{
			name: "repeatbrk_break",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.REPEATBRK().Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
			),
			stack: []any{int64(3)},
			exit:  0,
		},
		{
			name:  "repeatbrk_bad_body_consumes_top_only",
			code:  codeFromBuilders(t, execop.REPEATBRK().Serialize()),
			stack: []any{int64(3), int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "repeatbrk_bad_count_consumes_body_and_count",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.REPEATBRK().Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "repeatendbrk_break",
			code:  codeFromBuilders(t, execop.REPEATENDBRK().Serialize(), execop.RETALT().Serialize()),
			stack: []any{int64(3)},
			exit:  0,
		},
		{
			name:  "repeatendbrk_bad_count_consumes_count",
			code:  codeFromBuilders(t, execop.REPEATENDBRK().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "repeatend_bad_count_consumes_count_only",
			code:  codeFromBuilders(t, execop.REPEATEND().Serialize()),
			stack: []any{int64(11), testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "untilbrk_break",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.UNTILBRK().Serialize(),
				stackop.PUSHINT(big.NewInt(8)).Serialize(),
			),
			exit: 0,
		},
		{
			name:  "untilbrk_bad_body_consumes_top_only",
			code:  codeFromBuilders(t, execop.UNTILBRK().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "untilendbrk_break",
			code: codeFromBuilders(t,
				execop.UNTILENDBRK().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "whilebrk_break",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, stackop.PUSHINT(big.NewInt(1)).Serialize())).Serialize(),
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.WHILEBRK().Serialize(),
				stackop.PUSHINT(big.NewInt(9)).Serialize(),
			),
			exit: 0,
		},
		{
			name:  "whilebrk_bad_body_consumes_top_only",
			code:  codeFromBuilders(t, execop.WHILEBRK().Serialize()),
			stack: []any{int64(33), int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "whilebrk_bad_cond_consumes_body_and_cond",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.WHILEBRK().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "whileendbrk_break",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, stackop.PUSHINT(big.NewInt(1)).Serialize())).Serialize(),
				execop.WHILEENDBRK().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name:  "whileendbrk_bad_cond_consumes_top_only",
			code:  codeFromBuilders(t, execop.WHILEENDBRK().Serialize()),
			stack: []any{int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "whileend_bad_cond_consumes_top_only",
			code:  codeFromBuilders(t, execop.WHILEEND().Serialize()),
			stack: []any{int64(11), testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "againbrk_break",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retAltBody).Serialize(),
				execop.AGAINBRK().Serialize(),
			),
			exit: 0,
		},
		{
			name:  "againbrk_bad_body_consumes_top",
			code:  codeFromBuilders(t, execop.AGAINBRK().Serialize()),
			stack: []any{int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "againendbrk_break",
			code: codeFromBuilders(t,
				execop.AGAINENDBRK().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "atexit_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.ATEXIT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "atexit_keeps_existing_c0_save",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(intBody(1)).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETCONTCTR(0).Serialize(),
				stackop.PUSHCONT(intBody(2)).Serialize(),
				execop.POPCTR(0).Serialize(),
				execop.ATEXIT().Serialize(),
				execop.RET().Serialize(),
			),
			exit: 0,
		},
		{
			name: "atexitalt_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.ATEXITALT().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "setexitalt_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.SETEXITALT().Serialize(),
				execop.RETALT().Serialize(),
			),
			exit: 0,
		},
		{
			name: "thenret_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.ATEXIT().Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.THENRET().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "thenretalt_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.THENRETALT().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "invert_ret",
			code: codeFromBuilders(t,
				execop.INVERT().Serialize(),
				execop.RET().Serialize(),
			),
			exit: 1,
		},
		{
			name: "booleval_true",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retBody).Serialize(),
				execop.BOOLEVAL().Serialize(),
			),
			exit: 0,
		},
		{
			name: "booland_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.BOOLAND().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "composboth_runs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(retAltBody).Serialize(),
				stackop.PUSHCONT(push7Body).Serialize(),
				execop.COMPOSBOTH().Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "calldict_short",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.CALLDICT(5).Serialize(),
			),
			exit: 0,
		},
		{
			name: "calldict_long",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.CALLDICT(300).Serialize(),
			),
			exit: 0,
		},
		{
			name: "jmpdict",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.JMPDICT(8).Serialize(),
			),
			exit: 0,
		},
		{
			name: "preparedict",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.PREPAREDICT(9).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "calldict_long_max_id",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.CALLDICT(0x3fff).Serialize(),
			),
			exit: 0,
		},
		{
			name: "jmpdict_max_id",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.JMPDICT(0x3fff).Serialize(),
			),
			exit: 0,
		},
		{
			name: "preparedict_max_id",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(3).Serialize(),
				execop.PREPAREDICT(0x3fff).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "try_handler_typecheck",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				execop.TRY().Serialize(),
			),
			stack: []any{push7Slice},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "try_body_typecheck",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.TRY().Serialize(),
			),
			stack: []any{int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "retvarargs_bad_range",
			code:  codeFromBuilders(t, execop.RETVARARGS().Serialize()),
			stack: []any{int64(255)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name:  "repeat_short_stack_underflow_before_type",
			code:  codeFromBuilders(t, execop.REPEAT().Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "while_short_stack_underflow_before_type",
			code:  codeFromBuilders(t, execop.WHILE().Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "ifbitjmp_short_stack_underflow_before_type",
			code:  codeFromBuilders(t, execop.IFBITJMP(0).Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "booland_short_stack_underflow_before_type",
			code:  codeFromBuilders(t, execop.BOOLAND().Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "booland_bad_top_continuation_consumes_top_only",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHREF(testEmptyCell()).Serialize(),
				execop.BOOLAND().Serialize(),
			),
			exit: vmerr.CodeTypeCheck,
		},
		{
			name: "composboth_bad_base_continuation_consumes_both",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.COMPOSBOTH().Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "atexit_bad_continuation_consumes_top",
			code:  codeFromBuilders(t, execop.ATEXIT().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "thenret_bad_continuation_consumes_top",
			code:  codeFromBuilders(t, execop.THENRET().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "booleval_bad_continuation_consumes_top",
			code:  codeFromBuilders(t, execop.BOOLEVAL().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "setcontctr_short_stack_underflow_before_type",
			code:  codeFromBuilders(t, execop.SETCONTCTR(0).Serialize()),
			stack: []any{int64(0)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "setcontctr_bad_continuation_consumes_top_only",
			code:  codeFromBuilders(t, execop.SETCONTCTR(4).Serialize()),
			stack: []any{testEmptyCell(), int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "setcontctr_bad_c0_value_consumes_inputs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETCONTCTR(0).Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "savectr_c7_default_empty_tuple",
			code: codeFromBuilders(t, execop.SAVECTR(7).Serialize()),
			exit: 0,
		},
		{
			name:  "condselchk_true",
			code:  codeFromBuilders(t, execop.CONDSELCHK().Serialize()),
			stack: []any{int64(-1), int64(11), int64(22)},
			exit:  0,
		},
		{
			name:  "condselchk_false",
			code:  codeFromBuilders(t, execop.CONDSELCHK().Serialize()),
			stack: []any{int64(0), int64(11), int64(22)},
			exit:  0,
		},
		{
			name:  "condselchk_type_mismatch",
			code:  codeFromBuilders(t, execop.CONDSELCHK().Serialize()),
			stack: []any{int64(-1), int64(11), cell.BeginCell().EndCell()},
			exit:  7,
		},
		{
			name:  "condselchk_non_integer_condition_typecheck",
			code:  codeFromBuilders(t, execop.CONDSELCHK().Serialize()),
			stack: []any{cell.BeginCell().EndCell(), int64(11), int64(22)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "condselchk_short_stack_preserves_stack",
			code:  codeFromBuilders(t, execop.CONDSELCHK().Serialize()),
			stack: []any{int64(1), int64(2)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "popctrx_short_invalid_idx_underflow",
			code:  codeFromBuilders(t, execop.POPCTRX().Serialize()),
			stack: []any{int64(6)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "popctr_empty_underflow",
			code: codeFromBuilders(t, execop.POPCTR(4).Serialize()),
			exit: vmerr.CodeStackUnderflow,
		},
		{
			name: "setretctr_empty_underflow",
			code: codeFromBuilders(t, execop.SETRETCTR(4).Serialize()),
			exit: vmerr.CodeStackUnderflow,
		},
		{
			name: "setaltctr_empty_underflow",
			code: codeFromBuilders(t, execop.SETALTCTR(4).Serialize()),
			exit: vmerr.CodeStackUnderflow,
		},
		{
			name: "popsavectr_empty_underflow",
			code: codeFromBuilders(t, execop.POPSAVECTR(4).Serialize()),
			exit: vmerr.CodeStackUnderflow,
		},
		{
			name:  "popsavectr_c0_bad_value_consumes_value",
			code:  codeFromBuilders(t, execop.POPSAVECTR(0).Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "popsavectr_c4_bad_value_consumes_value",
			code:  codeFromBuilders(t, execop.POPSAVECTR(4).Serialize()),
			stack: []any{int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "pushctrx_bad_index_type_consumes_index",
			code:  codeFromBuilders(t, execop.PUSHCTRX().Serialize()),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "popctrx_bad_index_type_consumes_index_only",
			code:  codeFromBuilders(t, execop.POPCTRX().Serialize()),
			stack: []any{testEmptyCell(), testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "popctrx_bad_c4_value_consumes_inputs",
			code:  codeFromBuilders(t, execop.POPCTRX().Serialize()),
			stack: []any{int64(11), int64(4)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "setcontctrx_short_invalid_idx_underflow",
			code:  codeFromBuilders(t, execop.SETCONTCTRX().Serialize()),
			stack: []any{int64(1), int64(6)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "setcontctrx_bad_index_type_consumes_index_only",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHREF(testEmptyCell()).Serialize(),
				execop.SETCONTCTRX().Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "setcontctrx_bad_continuation_consumes_index_and_top_only",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(4)).Serialize(),
				execop.SETCONTCTRX().Serialize(),
			),
			stack: []any{testEmptyCell(), int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "setcontctrx_bad_c4_value_consumes_inputs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(4)).Serialize(),
				execop.SETCONTCTRX().Serialize(),
			),
			stack: []any{int64(11)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "setcontctrmanyx_short_invalid_mask_underflow",
			code:  codeFromBuilders(t, execop.SETCONTCTRMANYX().Serialize()),
			stack: []any{int64(64)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "setcontctrmanyx_bad_mask_range_consumes_mask",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(256)).Serialize(),
				execop.SETCONTCTRMANYX().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "setcontctrmanyx_c6_mask_consumes_mask_only",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1<<6)).Serialize(),
				execop.SETCONTCTRMANYX().Serialize(),
			),
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "setcontctrmanyx_bad_continuation_consumes_mask_and_value",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				execop.SETCONTCTRMANYX().Serialize(),
			),
			stack: []any{testEmptyCell()},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name: "savectr_duplicate_typecheck",
			code: codeFromBuilders(t,
				execop.SAVECTR(1).Serialize(),
				execop.SAVECTR(1).Serialize(),
			),
			exit:          vmerr.CodeTypeCheck,
			skipReference: contOpsDuplicateSaveReferenceSkip,
			goStack:       []any{},
		},
		{
			name: "popsavectr_duplicate_save_ignored",
			code: codeFromBuilders(t,
				execop.SAVECTR(1).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPSAVECTR(1).Serialize(),
			),
			exit: 0,
		},
		{
			name: "savebothctr_duplicate_save_ignored",
			code: codeFromBuilders(t,
				execop.SAVEALTCTR(1).Serialize(),
				execop.SAVEBOTHCTR(1).Serialize(),
			),
			exit: 0,
		},
		{
			name: "callref_missing_ref_invalid",
			code: codeFromOpcodes(t, 0xDB3C),
			exit: 6,
		},
		{
			name:  "jmpref_missing_ref_invalid_preserves_stack",
			code:  codeFromOpcodes(t, 0xDB3D),
			stack: []any{int64(11)},
			exit:  vmerr.CodeInvalidOpcode,
		},
	}

}

func runContParityCase(t *testing.T, tt contOpsParityCase) {
	t.Helper()

	code := prependRawMethodDrop(tt.code)
	goStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCode(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refRes, err := runReferenceCrossCode(code, cell.BeginCell().EndCell(), tuple.Tuple{}, refStack)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
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

func TestTVMCrossEmulatorContOpsVersionedEdges(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := contOpsVersionedEdgeCases(t)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_CONTOPS_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		for _, version := range versions {
			version := version
			t.Run(fmt.Sprintf("%s_v%d", tt.name, version), func(t *testing.T) {
				runContOpsVersionedEdgeCase(t, tt, version)
			})
		}
	}
}

func FuzzTVMCrossEmulatorContOpsVersionedEdges(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%contOpsVersionedEdgeCaseCount))
	}
	for i := 0; i < contOpsVersionedEdgeCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := contOpsVersionedEdgeCases(t)
		if len(tests) != contOpsVersionedEdgeCaseCount {
			t.Fatalf("contops versioned edge case count = %d, want %d", len(tests), contOpsVersionedEdgeCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runContOpsVersionedEdgeCase(t, tt, version)
	})
}

const contOpsVersionedEdgeCaseCount = 13

type contOpsVersionedEdgeCase struct {
	name          string
	code          *cell.Cell
	stack         []any
	exit          func(int) int32
	skipReference func(int) string
	goStack       []any
}

func contOpsVersionedEdgeCases(t *testing.T) []contOpsVersionedEdgeCase {
	t.Helper()

	emptyBody := codeFromBuilders(t)
	runvmChild := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize()).MustBeginParse()
	cellValue := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	invalidBefore := func(minVersion int) func(int) int32 {
		return func(version int) int32 {
			if version < minVersion {
				return int32(vmerr.CodeInvalidOpcode)
			}
			return 0
		}
	}
	typeCheckExit := func(int) int32 {
		return int32(vmerr.CodeTypeCheck)
	}
	duplicateSaveExit := func(version int) int32 {
		if version >= 14 {
			return 0
		}
		return typeCheckExit(version)
	}
	setContManyDuplicateExit := func(version int) int32 {
		if version < 9 {
			return int32(vmerr.CodeInvalidOpcode)
		}
		return duplicateSaveExit(version)
	}

	return []contOpsVersionedEdgeCase{
		{
			name:  "runvm",
			code:  codeFromBuilders(t, execop.RUNVM(0).Serialize()),
			stack: []any{int64(0), runvmChild},
			exit:  invalidBefore(4),
		},
		{
			name: "setcontctrmany",
			code: codeFromBuilders(t,
				stackop.PUSHREF(cellValue).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETCONTCTRMANY(1<<4).Serialize(),
				stackop.DROP().Serialize(),
			),
			exit: invalidBefore(9),
		},
		{
			name: "setcontctrmanyx",
			code: codeFromBuilders(t,
				stackop.PUSHREF(cellValue).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1<<4)).Serialize(),
				execop.SETCONTCTRMANYX().Serialize(),
				stackop.DROP().Serialize(),
			),
			exit: invalidBefore(9),
		},
		{
			name: "savectr_duplicate",
			code: codeFromBuilders(t,
				execop.SAVECTR(1).Serialize(),
				execop.SAVECTR(1).Serialize(),
			),
			exit:          duplicateSaveExit,
			skipReference: contOpsDuplicateSaveReferenceSkip,
			goStack:       []any{},
		},
		{
			name: "setretctr_duplicate",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETRETCTR(1).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETRETCTR(1).Serialize(),
			),
			exit:          duplicateSaveExit,
			skipReference: contOpsDuplicateSaveReferenceSkip,
			goStack:       []any{},
		},
		{
			name: "setaltctr_duplicate",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETALTCTR(1).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETALTCTR(1).Serialize(),
			),
			exit:          duplicateSaveExit,
			skipReference: contOpsDuplicateSaveReferenceSkip,
			goStack:       []any{},
		},
		{
			name: "savealtctr_duplicate",
			code: codeFromBuilders(t,
				execop.SAVEALTCTR(1).Serialize(),
				execop.SAVEALTCTR(1).Serialize(),
			),
			exit:          duplicateSaveExit,
			skipReference: contOpsDuplicateSaveReferenceSkip,
			goStack:       []any{},
		},
		{
			name: "setretctr_cell_duplicate",
			code: codeFromBuilders(t,
				stackop.PUSHREF(cellValue).Serialize(),
				execop.SETRETCTR(4).Serialize(),
				stackop.PUSHREF(cellValue).Serialize(),
				execop.SETRETCTR(4).Serialize(),
			),
			exit:          duplicateSaveExit,
			skipReference: contOpsDuplicateSaveReferenceSkip,
			goStack:       []any{},
		},
		{
			name: "savectr_cell_duplicate",
			code: codeFromBuilders(t,
				stackop.PUSHREF(cellValue).Serialize(),
				execop.POPCTR(4).Serialize(),
				execop.SAVECTR(4).Serialize(),
				execop.SAVECTR(4).Serialize(),
			),
			exit:          duplicateSaveExit,
			skipReference: contOpsDuplicateSaveReferenceSkip,
			goStack:       []any{},
		},
		{
			name: "savealtctr_cell_duplicate",
			code: codeFromBuilders(t,
				stackop.PUSHREF(cellValue).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHREF(cellValue).Serialize(),
				execop.SETALTCTR(4).Serialize(),
				execop.SAVEALTCTR(4).Serialize(),
			),
			exit:          duplicateSaveExit,
			skipReference: contOpsDuplicateSaveReferenceSkip,
			goStack:       []any{},
		},
		{
			name: "setretctr_duplicate_wrong_type",
			code: codeFromBuilders(t,
				stackop.PUSHREF(cellValue).Serialize(),
				execop.SETRETCTR(4).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETRETCTR(4).Serialize(),
			),
			exit: typeCheckExit,
		},
		{
			name: "setcontctrmany_duplicate",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETCONTCTRMANY(1<<1).Serialize(),
				execop.SETCONTCTRMANY(1<<1).Serialize(),
				stackop.DROP().Serialize(),
			),
			exit:          setContManyDuplicateExit,
			skipReference: contOpsDuplicateSaveReferenceSkip,
			goStack:       []any{},
		},
		{
			name: "setcontctrmanyx_duplicate",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.POPCTR(1).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				stackop.PUSHINT(big.NewInt(1<<1)).Serialize(),
				execop.SETCONTCTRMANYX().Serialize(),
				stackop.PUSHINT(big.NewInt(1<<1)).Serialize(),
				execop.SETCONTCTRMANYX().Serialize(),
				stackop.DROP().Serialize(),
			),
			exit:          setContManyDuplicateExit,
			skipReference: contOpsDuplicateSaveReferenceSkip,
			goStack:       []any{},
		},
	}
}

func runContOpsVersionedEdgeCase(t *testing.T, tt contOpsVersionedEdgeCase, version int) {
	t.Helper()

	skipReference := ""
	if tt.skipReference != nil {
		skipReference = tt.skipReference(version)
	}
	runContVersionedParityCase(t, tt.code, tt.stack, version, tt.exit(version), skipReference, tt.goStack)
}

func runContVersionedParityCase(t *testing.T, code *cell.Cell, stack []any, globalVersion int, wantExit int32, skipReference string, wantGoStack []any) {
	t.Helper()

	runContVersionedParityCaseWithExpected(t, code, stack, globalVersion, &wantExit, skipReference, wantGoStack)
}

func runContVersionedParityCaseWithoutExpected(t *testing.T, code *cell.Cell, stack []any, globalVersion int) {
	t.Helper()

	runContVersionedParityCaseWithExpected(t, code, stack, globalVersion, nil, "", nil)
}

func runContVersionedParityCaseWithExpected(t *testing.T, code *cell.Cell, stack []any, globalVersion int, wantExit *int32, skipReference string, wantGoStack []any) {
	t.Helper()

	code = prependRawMethodDrop(code)
	goStack, err := buildCrossStack(stack...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stack...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack, globalVersion)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	if skipReference != "" {
		if wantExit != nil && goRes.exitCode != *wantExit {
			t.Fatalf("unexpected go exit code: got=%d expected=%d", goRes.exitCode, *wantExit)
		}
		assertContSkippedGoStack(t, goRes.stack, wantGoStack)
		t.Skip(skipReference)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if wantExit != nil && (goRes.exitCode != *wantExit || refRes.exitCode != *wantExit) {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, *wantExit)
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

func assertContSkippedGoStack(t *testing.T, got *cell.Cell, want []any) {
	t.Helper()

	if want == nil {
		return
	}
	wantStack, err := buildCrossStack(want...)
	if err != nil {
		t.Fatalf("failed to build expected go stack: %v", err)
	}
	wantStackCell, err := stackToCell(wantStack)
	if err != nil {
		t.Fatalf("failed to serialize expected go stack: %v", err)
	}
	gotStackCell, err := normalizeStackCell(got)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	wantStackCell, err = normalizeStackCell(wantStackCell)
	if err != nil {
		t.Fatalf("failed to normalize expected go stack: %v", err)
	}
	if !bytes.Equal(gotStackCell.Hash(), wantStackCell.Hash()) {
		t.Fatalf("go stack mismatch:\ngo=%s\nwant=%s", gotStackCell.Dump(), wantStackCell.Dump())
	}
}
