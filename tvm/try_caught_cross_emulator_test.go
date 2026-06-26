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
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorCaughtTryEdges(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	handler := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(0xCAFE)).Serialize())
	tryCode := func(body *cell.Cell) *cell.Cell {
		return prependRawMethodDrop(codeFromBuilders(t,
			stackop.PUSHCONT(body).Serialize(),
			stackop.PUSHCONT(handler).Serialize(),
			execop.TRY().Serialize(),
		))
	}

	type testCase struct {
		name  string
		code  *cell.Cell
		stack []any
	}

	tests := []testCase{
		{
			name: "throw_no_arg",
			code: tryCode(codeFromBuilders(t,
				cell.BeginCell().MustStoreUInt(0xF225, 16),
			)),
			stack: []any{int64(777)},
		},
		{
			name: "throwarg",
			code: tryCode(codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(321)).Serialize(),
				cell.BeginCell().MustStoreUInt(0xF2C955, 24),
			)),
			stack: []any{int64(888)},
		},
		{
			name: "throwif_true",
			code: tryCode(codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(-1)).Serialize(),
				cell.BeginCell().MustStoreUInt(0xF261, 16),
			)),
			stack: []any{int64(999)},
		},
		{
			name: "ldu_underflow",
			code: tryCode(codeFromBuilders(t,
				cellsliceop.LDU(8).Serialize(),
			)),
			stack: []any{cell.BeginCell().EndCell().MustBeginParse(), int64(111)},
		},
		{
			name: "ldref_underflow",
			code: tryCode(codeFromBuilders(t,
				cellsliceop.LDREF().Serialize(),
			)),
			stack: []any{cell.BeginCell().MustStoreUInt(0xA, 4).EndCell().MustBeginParse(), int64(222)},
		},
		{
			name: "dictset_short_key_underflow",
			code: tryCode(codeFromBuilders(t,
				cell.BeginCell().MustStoreUInt(0xF412, 16),
			)),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().MustBeginParse(),
				cell.BeginCell().MustStoreUInt(0x1, 4).EndCell().MustBeginParse(),
				nil,
				int64(8),
			},
		},
		{
			name: "dictsetgetoptref_short_key_underflow",
			code: tryCode(codeFromBuilders(t,
				cell.BeginCell().MustStoreUInt(0xF46D, 16),
			)),
			stack: []any{
				cell.BeginCell().EndCell(),
				cell.BeginCell().MustStoreUInt(0x1, 4).EndCell().MustBeginParse(),
				nil,
				int64(8),
			},
		},
		{
			name: "arith_min_nan_top",
			code: tryCode(codeFromBuilders(t,
				mathop.MIN().Serialize(),
			)),
			stack: []any{int64(7), vm.NaN{}},
		},
		{
			name: "arith_muldiv_nan_top",
			code: tryCode(codeFromBuilders(t,
				mathop.MULDIV().Serialize(),
			)),
			stack: []any{int64(2), int64(3), vm.NaN{}},
		},
		{
			name: "arith_adddivmod_nan_middle",
			code: tryCode(codeFromBuilders(t,
				mathop.ADDDIVMOD().Serialize(),
			)),
			stack: []any{int64(5), vm.NaN{}, int64(2)},
		},
		{
			name: "arith_lshiftdiv_nan_divisor",
			code: tryCode(codeFromBuilders(t,
				mathop.LSHIFTDIV().Serialize(),
			)),
			stack: []any{int64(5), vm.NaN{}, int64(1)},
		},
		{
			name: "arith_addrshiftmod_nan_addend",
			code: tryCode(codeFromBuilders(t,
				mathop.ADDRSHIFTMOD().Serialize(),
			)),
			stack: []any{int64(11), vm.NaN{}, int64(5)},
		},
		{
			name: "arith_muladdrshiftmod_nan_addend",
			code: tryCode(codeFromBuilders(t,
				mathop.MULADDRSHIFTMOD().Serialize(),
			)),
			stack: []any{int64(11), int64(3), vm.NaN{}, int64(5)},
		},
		{
			name: "arith_mulmodpow2_nan_multiplier",
			code: tryCode(codeFromBuilders(t,
				mathop.MULMODPOW2_VAR().Serialize(),
			)),
			stack: []any{int64(11), vm.NaN{}, int64(5)},
		},
		{
			name: "fee_getgasfee_short_stack_keeps_arg",
			code: tryCode(codeFromBuilders(t,
				funcsop.GETGASFEE().Serialize(),
			)),
			stack: []any{int64(250)},
		},
		{
			name: "fee_getgasfee_nan_gas_range",
			code: tryCode(codeFromBuilders(t,
				funcsop.GETGASFEE().Serialize(),
			)),
			stack: []any{vm.NaN{}, int64(0)},
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

			goRes, err := runGoCrossCode(tt.code, testEmptyCell(), tuple.Tuple{}, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(tt.code, testEmptyCell(), tuple.Tuple{}, refStack)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != 0 || refRes.exitCode != 0 {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=0", goRes.exitCode, refRes.exitCode)
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

func TestTVMCrossEmulatorCaughtTryAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := tryCaughtVersionedCases(t, 777)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_TRY_CAUGHT_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runTryCaughtVersionedCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorCaughtTryGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%tryCaughtVersionedCaseCount), uint16(777+version))
	}
	for i := 0; i < tryCaughtVersionedCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i), uint16(0x20+i))
	}
	f.Add(uint8(255), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8, rawValue uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := tryCaughtVersionedCases(t, rawValue)
		if len(tests) != tryCaughtVersionedCaseCount {
			t.Fatalf("try caught versioned case count = %d, want %d", len(tests), tryCaughtVersionedCaseCount)
		}
		runTryCaughtVersionedCase(t, tests[int(rawCase)%len(tests)], version)
	})
}

type tryCaughtVersionedCase struct {
	name  string
	code  *cell.Cell
	stack []any
}

const tryCaughtVersionedCaseCount = 16

func tryCaughtVersionedCases(t *testing.T, value uint16) []tryCaughtVersionedCase {
	t.Helper()

	stackValue := int64(value)
	emptySlice := cell.BeginCell().EndCell().MustBeginParse()
	shortSlice := cell.BeginCell().MustStoreUInt(uint64(value&0xf), 4).EndCell().MustBeginParse()
	valueSlice := cell.BeginCell().MustStoreUInt(uint64(value&0xff), 8).EndCell().MustBeginParse()

	return []tryCaughtVersionedCase{
		{
			name: "throw_no_arg",
			code: tryCaughtCode(t, codeFromBuilders(t,
				cell.BeginCell().MustStoreUInt(0xF225, 16),
			)),
			stack: []any{stackValue},
		},
		{
			name: "throwarg",
			code: tryCaughtCode(t, codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(stackValue+321)).Serialize(),
				cell.BeginCell().MustStoreUInt(0xF2C955, 24),
			)),
			stack: []any{stackValue + 888},
		},
		{
			name: "throwif_true",
			code: tryCaughtCode(t, codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(-1)).Serialize(),
				cell.BeginCell().MustStoreUInt(0xF261, 16),
			)),
			stack: []any{stackValue + 999},
		},
		{
			name: "ldu_underflow",
			code: tryCaughtCode(t, codeFromBuilders(t,
				cellsliceop.LDU(8).Serialize(),
			)),
			stack: []any{emptySlice, stackValue + 111},
		},
		{
			name: "ldref_underflow",
			code: tryCaughtCode(t, codeFromBuilders(t,
				cellsliceop.LDREF().Serialize(),
			)),
			stack: []any{shortSlice, stackValue + 222},
		},
		{
			name: "dictset_short_key_underflow",
			code: tryCaughtCode(t, codeFromBuilders(t,
				cell.BeginCell().MustStoreUInt(0xF412, 16),
			)),
			stack: []any{
				valueSlice,
				shortSlice,
				nil,
				int64(8),
			},
		},
		{
			name: "dictsetgetoptref_short_key_underflow",
			code: tryCaughtCode(t, codeFromBuilders(t,
				cell.BeginCell().MustStoreUInt(0xF46D, 16),
			)),
			stack: []any{
				cell.BeginCell().EndCell(),
				shortSlice,
				nil,
				int64(8),
			},
		},
		{
			name: "arith_min_nan_top",
			code: tryCaughtCode(t, codeFromBuilders(t,
				mathop.MIN().Serialize(),
			)),
			stack: []any{stackValue + 7, vm.NaN{}},
		},
		{
			name: "arith_muldiv_nan_top",
			code: tryCaughtCode(t, codeFromBuilders(t,
				mathop.MULDIV().Serialize(),
			)),
			stack: []any{stackValue + 2, int64(3), vm.NaN{}},
		},
		{
			name: "arith_adddivmod_nan_middle",
			code: tryCaughtCode(t, codeFromBuilders(t,
				mathop.ADDDIVMOD().Serialize(),
			)),
			stack: []any{stackValue + 5, vm.NaN{}, int64(2)},
		},
		{
			name: "arith_lshiftdiv_nan_divisor",
			code: tryCaughtCode(t, codeFromBuilders(t,
				mathop.LSHIFTDIV().Serialize(),
			)),
			stack: []any{stackValue + 5, vm.NaN{}, int64(1)},
		},
		{
			name: "arith_addrshiftmod_nan_addend",
			code: tryCaughtCode(t, codeFromBuilders(t,
				mathop.ADDRSHIFTMOD().Serialize(),
			)),
			stack: []any{stackValue + 11, vm.NaN{}, int64(5)},
		},
		{
			name: "arith_muladdrshiftmod_nan_addend",
			code: tryCaughtCode(t, codeFromBuilders(t,
				mathop.MULADDRSHIFTMOD().Serialize(),
			)),
			stack: []any{stackValue + 11, int64(3), vm.NaN{}, int64(5)},
		},
		{
			name: "arith_mulmodpow2_nan_multiplier",
			code: tryCaughtCode(t, codeFromBuilders(t,
				mathop.MULMODPOW2_VAR().Serialize(),
			)),
			stack: []any{stackValue + 11, vm.NaN{}, int64(5)},
		},
		{
			name: "fee_getgasfee_short_stack_keeps_arg",
			code: tryCaughtCode(t, codeFromBuilders(t,
				funcsop.GETGASFEE().Serialize(),
			)),
			stack: []any{stackValue + 250},
		},
		{
			name: "fee_getgasfee_nan_gas_range",
			code: tryCaughtCode(t, codeFromBuilders(t,
				funcsop.GETGASFEE().Serialize(),
			)),
			stack: []any{vm.NaN{}, int64(value & 1)},
		},
	}
}

func tryCaughtCode(t *testing.T, body *cell.Cell) *cell.Cell {
	t.Helper()

	handler := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(0xCAFE)).Serialize())
	return prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHCONT(body).Serialize(),
		stackop.PUSHCONT(handler).Serialize(),
		execop.TRY().Serialize(),
	))
}

func runTryCaughtVersionedCase(t *testing.T, tt tryCaughtVersionedCase, globalVersion int) {
	t.Helper()

	goStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(tt.code, testEmptyCell(), tuple.Tuple{}, goStack, globalVersion)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refRes, err := runReferenceCrossCodeViaEmulator(tt.code, testEmptyCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != 0 || refRes.exitCode != 0 {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=0", goRes.exitCode, refRes.exitCode)
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
