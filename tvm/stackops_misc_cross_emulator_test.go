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
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorStackOpsMisc(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	refCell := cell.BeginCell().MustStoreUInt(0xABCD, 16).EndCell()
	refSlice := cell.BeginCell().MustStoreUInt(0x5, 3).MustStoreRef(refCell).EndCell().MustBeginParse()
	pushRefContBody := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(77)).Serialize())
	pushCont16ByteBody := cell.BeginCell().MustStoreSlice(make([]byte, 16), 16*8).EndCell()
	pushContFourRefsBody := cell.BeginCell().
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell()
	negativeLong := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))
	minTVMInt := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))
	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	pushIntLongOverflow := cell.BeginCell().
		MustStoreUInt(0x82, 8).
		MustStoreUInt(30, 5).
		MustStoreUInt(1, 2).
		MustStoreSlice(make([]byte, 33), 257)
	debugSlice := cell.BeginCell().MustStoreSlice([]byte("debug"), 40).ToSlice()

	tests := []struct {
		name  string
		code  *cell.Cell
		stack []any
		exit  int32
	}{
		{
			name:  "condsel_true_keeps_x",
			code:  codeFromBuilders(t, stackop.CONDSEL().Serialize()),
			stack: []any{int64(-1), int64(11), int64(22)},
			exit:  0,
		},
		{
			name:  "condsel_false_keeps_y",
			code:  codeFromBuilders(t, stackop.CONDSEL().Serialize()),
			stack: []any{int64(0), refCell, int64(22)},
			exit:  0,
		},
		{
			name: "pushint_negative_8bit_boundary",
			code: codeFromBuilders(t, stackop.PUSHINT(big.NewInt(-128)).Serialize()),
			exit: 0,
		},
		{
			name: "pushint_negative_long_sign_extension",
			code: codeFromBuilders(t, stackop.PUSHINT(negativeLong).Serialize()),
			exit: 0,
		},
		{
			name: "pushint_min_tvm_int",
			code: codeFromBuilders(t, stackop.PUSHINT(minTVMInt).Serialize()),
			exit: 0,
		},
		{
			name: "pushint_max_tvm_int",
			code: codeFromBuilders(t, stackop.PUSHINT(maxTVMInt).Serialize()),
			exit: 0,
		},
		{
			name: "pushint_raw_long_259_noncanonical_overflow",
			code: codeFromBuilders(t, pushIntLongOverflow),
			exit: vmerr.CodeIntOverflow,
		},
		{
			name: "pushrefslice_ref_payload",
			code: codeFromBuilders(t, stackop.PUSHREFSLICE(refSlice).Serialize()),
			exit: 0,
		},
		{
			name: "pushref_ref_payload",
			code: codeFromBuilders(t, stackop.PUSHREF(refCell).Serialize()),
			exit: 0,
		},
		{
			name: "pushrefcont_execute",
			code: codeFromBuilders(t,
				stackop.PUSHREFCONT(pushRefContBody).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "pushcont_16_byte_big_form_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(pushCont16ByteBody).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "pushcont_four_refs_ref_form_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(pushContFourRefsBody).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			exit: 0,
		},
		{
			name: "dictpushconst_ref_and_prefix",
			code: codeFromBuilders(t, stackop.DICTPUSHCONST(refCell).Serialize()),
			exit: 0,
		},
		{
			name: "dictpushconst_missing_ref_stack_underflow",
			code: cell.BeginCell().
				MustStoreSlice([]byte{0xF4, 0xA4}, 13).
				MustStoreBoolBit(false).
				MustStoreUInt(0, 10).
				EndCell(),
			exit: vmerr.CodeStackUnderflow,
		},
		{
			name: "dictpushconst_missing_prefix_bits_invalid_opcode",
			code: cell.BeginCell().
				MustStoreSlice([]byte{0xF4, 0xA4}, 13).
				MustStoreBoolBit(true).
				MustStoreRef(cell.BeginCell().EndCell()).
				EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name:  "dumpstk_keeps_stack",
			code:  codeFromBuilders(t, stackop.DUMPSTK().Serialize()),
			stack: []any{int64(1), refCell},
			exit:  0,
		},
		{
			name:  "dump_absent_keeps_stack",
			code:  codeFromBuilders(t, stackop.DUMP(3).Serialize()),
			stack: []any{int64(1)},
			exit:  0,
		},
		{
			name:  "debug_keeps_stack",
			code:  codeFromBuilders(t, stackop.DEBUG(42).Serialize()),
			stack: []any{int64(1)},
			exit:  0,
		},
		{
			name:  "strdump_slice_keeps_stack",
			code:  codeFromBuilders(t, stackop.STRDUMP().Serialize()),
			stack: []any{debugSlice},
			exit:  0,
		},
		{
			name:  "condsel_nan_condition_overflow",
			code:  codeFromBuilders(t, stackop.CONDSEL().Serialize()),
			stack: []any{vm.NaN{}, int64(11), int64(22)},
			exit:  vmerr.CodeIntOverflow,
		},
		{
			name:  "blkswx_short_stack_nan_y",
			code:  codeFromBuilders(t, stackop.BLKSWX().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "blkswap_fixed_underflow_keeps_stack",
			code:  codeFromBuilders(t, stackop.BLKSWAP(2, 2).Serialize()),
			stack: []any{int64(1), int64(2), int64(3)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "blkswx_negative_y_leaves_x",
			code:  codeFromBuilders(t, stackop.BLKSWX().Serialize()),
			stack: []any{int64(1), int64(2), int64(3), int64(2), int64(-1)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name:  "blkswx_negative_x_consumes_y_and_x",
			code:  codeFromBuilders(t, stackop.BLKSWX().Serialize()),
			stack: []any{int64(1), int64(2), int64(3), int64(-1), int64(1)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name:  "blkswx_typecheck_x_consumes_y_and_x",
			code:  codeFromBuilders(t, stackop.BLKSWX().Serialize()),
			stack: []any{int64(1), int64(2), int64(3), refCell, int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "blkswx_too_large_counts_consume_both",
			code:  codeFromBuilders(t, stackop.BLKSWX().Serialize()),
			stack: []any{int64(1), int64(2), int64(3), int64(2), int64(2)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "revx_short_stack_nan_y",
			code:  codeFromBuilders(t, stackop.REVX().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "debugstr_keeps_stack",
			code: codeFromBuilders(t, stackop.DEBUGSTR([]byte("hello")).Serialize()),
			exit: 0,
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

			code := prependRawMethodDrop(tt.code)
			goRes, err := runGoCrossCode(code, testEmptyCell(), tuple.Tuple{}, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(code, testEmptyCell(), tuple.Tuple{}, refStack)
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

func TestTVMCrossEmulatorStackOpsMiscAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := stackOpsMiscVersionedCases(t, 77)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_STACKOPS_MISC_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runStackOpsMiscVersionedCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorStackOpsMiscGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%stackOpsMiscVersionedCaseCount), uint16(77+version))
	}
	for i := 0; i < stackOpsMiscVersionedCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i), uint16(0x30+i))
	}
	f.Add(uint8(255), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8, rawValue uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := stackOpsMiscVersionedCases(t, rawValue)
		if len(tests) != stackOpsMiscVersionedCaseCount {
			t.Fatalf("stackops misc versioned case count = %d, want %d", len(tests), stackOpsMiscVersionedCaseCount)
		}
		runStackOpsMiscVersionedCase(t, tests[int(rawCase)%len(tests)], version)
	})
}

type stackOpsMiscVersionedCase struct {
	name  string
	code  *cell.Cell
	stack []any
	exit  int32
}

const stackOpsMiscVersionedCaseCount = 28

func stackOpsMiscVersionedCases(t *testing.T, value uint16) []stackOpsMiscVersionedCase {
	t.Helper()

	stackValue := int64(value)
	refCell := cell.BeginCell().MustStoreUInt(uint64(value), 16).EndCell()
	refSlice := cell.BeginCell().MustStoreUInt(uint64(value&0x7), 3).MustStoreRef(refCell).EndCell().MustBeginParse()
	pushRefContBody := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(stackValue)).Serialize())
	pushCont16ByteBody := cell.BeginCell().MustStoreSlice(make([]byte, 16), 16*8).EndCell()
	pushContFourRefsBody := cell.BeginCell().
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell()
	negativeLong := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))
	minTVMInt := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))
	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	pushIntLongOverflow := cell.BeginCell().
		MustStoreUInt(0x82, 8).
		MustStoreUInt(30, 5).
		MustStoreUInt(1, 2).
		MustStoreSlice(make([]byte, 33), 257)
	debugSlice := cell.BeginCell().MustStoreSlice([]byte("debug"), 40).ToSlice()

	return []stackOpsMiscVersionedCase{
		{
			name:  "condsel_true_keeps_x",
			code:  codeFromBuilders(t, stackop.CONDSEL().Serialize()),
			stack: []any{int64(-1), stackValue + 11, stackValue + 22},
		},
		{
			name:  "condsel_false_keeps_y",
			code:  codeFromBuilders(t, stackop.CONDSEL().Serialize()),
			stack: []any{int64(0), refCell, stackValue + 22},
		},
		{
			name: "pushint_negative_8bit_boundary",
			code: codeFromBuilders(t, stackop.PUSHINT(big.NewInt(-128)).Serialize()),
		},
		{
			name: "pushint_negative_long_sign_extension",
			code: codeFromBuilders(t, stackop.PUSHINT(negativeLong).Serialize()),
		},
		{
			name: "pushint_min_tvm_int",
			code: codeFromBuilders(t, stackop.PUSHINT(minTVMInt).Serialize()),
		},
		{
			name: "pushint_max_tvm_int",
			code: codeFromBuilders(t, stackop.PUSHINT(maxTVMInt).Serialize()),
		},
		{
			name: "pushint_raw_long_259_noncanonical_overflow",
			code: codeFromBuilders(t, pushIntLongOverflow),
			exit: vmerr.CodeIntOverflow,
		},
		{
			name: "pushrefslice_ref_payload",
			code: codeFromBuilders(t, stackop.PUSHREFSLICE(refSlice).Serialize()),
		},
		{
			name: "pushref_ref_payload",
			code: codeFromBuilders(t, stackop.PUSHREF(refCell).Serialize()),
		},
		{
			name: "pushrefcont_execute",
			code: codeFromBuilders(t,
				stackop.PUSHREFCONT(pushRefContBody).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "pushcont_16_byte_big_form_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(pushCont16ByteBody).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "pushcont_four_refs_ref_form_execute",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(pushContFourRefsBody).Serialize(),
				execop.EXECUTE().Serialize(),
			),
		},
		{
			name: "dictpushconst_ref_and_prefix",
			code: codeFromBuilders(t, stackop.DICTPUSHCONST(refCell).Serialize()),
		},
		{
			name: "dictpushconst_missing_ref_stack_underflow",
			code: cell.BeginCell().
				MustStoreSlice([]byte{0xF4, 0xA4}, 13).
				MustStoreBoolBit(false).
				MustStoreUInt(0, 10).
				EndCell(),
			exit: vmerr.CodeStackUnderflow,
		},
		{
			name: "dictpushconst_missing_prefix_bits_invalid_opcode",
			code: cell.BeginCell().
				MustStoreSlice([]byte{0xF4, 0xA4}, 13).
				MustStoreBoolBit(true).
				MustStoreRef(cell.BeginCell().EndCell()).
				EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name:  "dumpstk_keeps_stack",
			code:  codeFromBuilders(t, stackop.DUMPSTK().Serialize()),
			stack: []any{stackValue, refCell},
		},
		{
			name:  "dump_absent_keeps_stack",
			code:  codeFromBuilders(t, stackop.DUMP(3).Serialize()),
			stack: []any{stackValue},
		},
		{
			name:  "debug_keeps_stack",
			code:  codeFromBuilders(t, stackop.DEBUG(42).Serialize()),
			stack: []any{stackValue},
		},
		{
			name:  "strdump_slice_keeps_stack",
			code:  codeFromBuilders(t, stackop.STRDUMP().Serialize()),
			stack: []any{debugSlice},
		},
		{
			name:  "condsel_nan_condition_overflow",
			code:  codeFromBuilders(t, stackop.CONDSEL().Serialize()),
			stack: []any{vm.NaN{}, stackValue + 11, stackValue + 22},
			exit:  vmerr.CodeIntOverflow,
		},
		{
			name:  "blkswx_short_stack_nan_y",
			code:  codeFromBuilders(t, stackop.BLKSWX().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "blkswap_fixed_underflow_keeps_stack",
			code:  codeFromBuilders(t, stackop.BLKSWAP(2, 2).Serialize()),
			stack: []any{stackValue + 1, stackValue + 2, stackValue + 3},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "blkswx_negative_y_leaves_x",
			code:  codeFromBuilders(t, stackop.BLKSWX().Serialize()),
			stack: []any{stackValue + 1, stackValue + 2, stackValue + 3, int64(2), int64(-1)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name:  "blkswx_negative_x_consumes_y_and_x",
			code:  codeFromBuilders(t, stackop.BLKSWX().Serialize()),
			stack: []any{stackValue + 1, stackValue + 2, stackValue + 3, int64(-1), int64(1)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name:  "blkswx_typecheck_x_consumes_y_and_x",
			code:  codeFromBuilders(t, stackop.BLKSWX().Serialize()),
			stack: []any{stackValue + 1, stackValue + 2, stackValue + 3, refCell, int64(1)},
			exit:  vmerr.CodeTypeCheck,
		},
		{
			name:  "blkswx_too_large_counts_consume_both",
			code:  codeFromBuilders(t, stackop.BLKSWX().Serialize()),
			stack: []any{stackValue + 1, stackValue + 2, stackValue + 3, int64(2), int64(2)},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name:  "revx_short_stack_nan_y",
			code:  codeFromBuilders(t, stackop.REVX().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  vmerr.CodeStackUnderflow,
		},
		{
			name: "debugstr_keeps_stack",
			code: codeFromBuilders(t, stackop.DEBUGSTR([]byte("hello")).Serialize()),
		},
	}
}

func runStackOpsMiscVersionedCase(t *testing.T, tt stackOpsMiscVersionedCase, globalVersion int) {
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

	goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), tuple.Tuple{}, goStack, globalVersion)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, *refCfg)
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
}
