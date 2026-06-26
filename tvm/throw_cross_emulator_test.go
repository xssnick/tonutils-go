//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorThrowOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	type testCase struct {
		name         string
		code         *cell.Cell
		stack        []any
		exit         int32
		noStackCheck bool
	}

	badCell := cell.BeginCell().EndCell()

	tests := []testCase{
		{
			name: "throwif_true",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF261, 16)),
			stack: []any{
				int64(-1),
			},
			exit:         0x21,
			noStackCheck: true,
		},
		{
			name: "throwif_false_skips",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF25B, 16)),
			stack: []any{
				int64(777),
				int64(0),
			},
			exit: 0,
		},
		{
			name: "throwargif_true",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2D955, 24)),
			stack: []any{
				int64(321),
				int64(-1),
			},
			exit:         0x155,
			noStackCheck: true,
		},
		{
			name: "throwargifnot_true_skips",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2E888, 24)),
			stack: []any{
				int64(999),
				int64(321),
				int64(-1),
			},
			exit: 0,
		},
		{
			name: "throwanyif_true",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F2, 16)),
			stack: []any{
				int64(0x222),
				int64(-1),
			},
			exit:         0x222,
			noStackCheck: true,
		},
		{
			name: "throwarganyifnot_false",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F5, 16)),
			stack: []any{
				int64(0x77),
				int64(0x222),
				int64(0),
			},
			exit:         0x222,
			noStackCheck: true,
		},
		{
			name:         "throw_13_is_handled_exception_not_oog",
			code:         codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF20D, 16)),
			exit:         int32(vmerr.CodeOutOfGas),
			noStackCheck: true,
		},
		{
			name: "default_c2_empty_stack_returns_pop_error",
			code: codeFromBuilders(t,
				execop.PUSHCTR(2).Serialize(),
				execop.JMPX().Serialize(),
			),
			exit:         int32(vmerr.CodeStackUnderflow),
			noStackCheck: true,
		},
		{
			name: "throwif_bad_condition_consumes_condition_only",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF261, 16)),
			stack: []any{
				int64(777),
				badCell,
			},
			exit: int32(vmerr.CodeTypeCheck),
		},
		{
			name: "throwargif_bad_condition_leaves_arg",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2D955, 24)),
			stack: []any{
				int64(777),
				int64(321),
				badCell,
			},
			exit: int32(vmerr.CodeTypeCheck),
		},
		{
			name: "throwany_bad_condition_consumes_condition_only",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F2, 16)),
			stack: []any{
				int64(777),
				int64(0x222),
				badCell,
			},
			exit: int32(vmerr.CodeTypeCheck),
		},
		{
			name: "throwany_bad_exception_consumes_condition_and_exception",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F2, 16)),
			stack: []any{
				int64(777),
				badCell,
				int64(-1),
			},
			exit: int32(vmerr.CodeTypeCheck),
		},
		{
			name: "throwany_exception_range_consumes_exception",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F0, 16)),
			stack: []any{
				int64(777),
				int64(0x10000),
			},
			exit: int32(vmerr.CodeRangeCheck),
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

			if tt.noStackCheck {
				return
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

func TestTVMCrossEmulatorThrowOpsAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := throwVersionedParityCases(t, 777)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_THROW_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runThrowVersionedParityCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorThrowOpsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%throwVersionedParityCaseCount), uint16(777+version))
	}
	for i := 0; i < throwVersionedParityCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i), uint16(0x20+i))
	}
	f.Add(uint8(255), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8, rawValue uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := throwVersionedParityCases(t, rawValue)
		if len(tests) != throwVersionedParityCaseCount {
			t.Fatalf("throw versioned parity case count = %d, want %d", len(tests), throwVersionedParityCaseCount)
		}
		runThrowVersionedParityCase(t, tests[int(rawCase)%len(tests)], version)
	})
}

type throwVersionedParityCase struct {
	name         string
	code         *cell.Cell
	stack        []any
	exit         int32
	noStackCheck bool
}

const throwVersionedParityCaseCount = 13

func throwVersionedParityCases(t *testing.T, value uint16) []throwVersionedParityCase {
	t.Helper()

	badCell := cell.BeginCell().EndCell()
	stackValue := int64(value)
	anyExit := int32(1 + int(value)%0xfffe)

	return []throwVersionedParityCase{
		{
			name: "throwif_false_skips",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF25B, 16)),
			stack: []any{
				stackValue,
				int64(0),
			},
			exit: 0,
		},
		{
			name: "throwif_true",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF261, 16)),
			stack: []any{
				int64(-1),
			},
			exit:         0x21,
			noStackCheck: true,
		},
		{
			name: "throwargif_true",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2D955, 24)),
			stack: []any{
				stackValue + 321,
				int64(-1),
			},
			exit:         0x155,
			noStackCheck: true,
		},
		{
			name: "throwargifnot_true_skips",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2E888, 24)),
			stack: []any{
				stackValue + 999,
				stackValue + 321,
				int64(-1),
			},
			exit: 0,
		},
		{
			name: "throwanyif_true",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F2, 16)),
			stack: []any{
				int64(anyExit),
				int64(-1),
			},
			exit:         anyExit,
			noStackCheck: true,
		},
		{
			name: "throwarganyifnot_false",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F5, 16)),
			stack: []any{
				stackValue + 0x77,
				int64(anyExit),
				int64(0),
			},
			exit:         anyExit,
			noStackCheck: true,
		},
		{
			name:         "throw_13_is_handled_exception_not_oog",
			code:         codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF20D, 16)),
			exit:         int32(vmerr.CodeOutOfGas),
			noStackCheck: true,
		},
		{
			name: "default_c2_empty_stack_returns_pop_error",
			code: codeFromBuilders(t,
				execop.PUSHCTR(2).Serialize(),
				execop.JMPX().Serialize(),
			),
			exit:         int32(vmerr.CodeStackUnderflow),
			noStackCheck: true,
		},
		{
			name: "throwif_bad_condition_consumes_condition_only",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF261, 16)),
			stack: []any{
				stackValue,
				badCell,
			},
			exit: int32(vmerr.CodeTypeCheck),
		},
		{
			name: "throwargif_bad_condition_leaves_arg",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2D955, 24)),
			stack: []any{
				stackValue,
				stackValue + 321,
				badCell,
			},
			exit: int32(vmerr.CodeTypeCheck),
		},
		{
			name: "throwany_bad_condition_consumes_condition_only",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F2, 16)),
			stack: []any{
				stackValue,
				int64(anyExit),
				badCell,
			},
			exit: int32(vmerr.CodeTypeCheck),
		},
		{
			name: "throwany_bad_exception_consumes_condition_and_exception",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F2, 16)),
			stack: []any{
				stackValue,
				badCell,
				int64(-1),
			},
			exit: int32(vmerr.CodeTypeCheck),
		},
		{
			name: "throwany_exception_range_consumes_exception",
			code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xF2F0, 16)),
			stack: []any{
				stackValue,
				int64(0x10000),
			},
			exit: int32(vmerr.CodeRangeCheck),
		},
	}
}

func runThrowVersionedParityCase(t *testing.T, tt throwVersionedParityCase, globalVersion int) {
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

	if tt.noStackCheck {
		return
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
