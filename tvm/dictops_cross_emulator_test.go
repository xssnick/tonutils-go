//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	dictop "github.com/xssnick/tonutils-go/tvm/op/dict"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const dictOpsParityCaseCount = 19

type dictOpsParityCase struct {
	name  string
	code  *cell.Cell
	stack []any
	exit  int32
	c7    tuple.Tuple
}

func TestTVMCrossEmulatorDictOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := dictOpsParityCases(t)
	if len(tests) != dictOpsParityCaseCount {
		t.Fatalf("dictops parity case count = %d, want %d", len(tests), dictOpsParityCaseCount)
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runDictParityCase(t, tt)
		})
	}
}

func TestTVMCrossEmulatorDictOpsAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := dictOpsParityCases(t)
	if len(tests) != dictOpsParityCaseCount {
		t.Fatalf("dictops parity case count = %d, want %d", len(tests), dictOpsParityCaseCount)
	}
	versions := crossEmulatorVersionAuditVersions(t, "TVM_DICTOPS_CORE_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runDictVersionedParityCase(t, tt.code, tt.stack, version, tt.exit)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorDictOpsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%dictOpsParityCaseCount))
	}
	for i := 0; i < dictOpsParityCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := dictOpsParityCases(t)
		if len(tests) != dictOpsParityCaseCount {
			t.Fatalf("dictops parity case count = %d, want %d", len(tests), dictOpsParityCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runDictVersionedParityCase(t, tt.code, tt.stack, version, tt.exit)
	})
}

func dictOpsParityCases(t *testing.T) []dictOpsParityCase {
	t.Helper()

	refDict := cell.NewDict(8)
	refValue := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	if _, err := refDict.SetBuilderWithMode(mustKeyCell(t, 0x10, 8), cell.BeginCell().MustStoreRef(refValue), cell.DictSetModeSet); err != nil {
		t.Fatalf("failed to seed ref dict: %v", err)
	}
	setGetOptRefValue := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()

	prefixValue := cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()
	prefixDict := mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
		0b10: {bits: 2, value: prefixValue},
	})

	jumpContDict := cell.NewDict(8)
	if _, err := jumpContDict.SetWithMode(mustKeyCell(t, 0x2A, 8), codeFromOpcodes(t, 0x70|7), cell.DictSetModeSet); err != nil {
		t.Fatalf("failed to seed jump continuation dict: %v", err)
	}

	switchDict := mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
		0b10: {bits: 2, value: codeFromOpcodes(t, 0x70|9)},
	})

	return []dictOpsParityCase{
		{
			name:  "lddicts_plain",
			code:  codeFromOpcodes(t, 0xF402),
			stack: []any{cell.BeginCell().MustStoreMaybeRef(mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8)).ToSlice()},
			exit:  0,
		},
		{
			name:  "lddict_plain",
			code:  codeFromOpcodes(t, 0xF404),
			stack: []any{cell.BeginCell().MustStoreMaybeRef(mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8)).ToSlice()},
			exit:  0,
		},
		{
			name:  "plddictq_invalid",
			code:  codeFromOpcodes(t, 0xF407),
			stack: []any{cell.BeginCell().EndCell().MustBeginParse()},
			exit:  0,
		},
		{
			name:  "dictget_hit",
			code:  codeFromOpcodes(t, 0xF40A),
			stack: []any{mustSliceKey(t, 0x12, 8), mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8), int64(8)},
			exit:  0,
		},
		{
			name:  "dictiget_out_of_range_miss",
			code:  codeFromOpcodes(t, 0xF40C),
			stack: []any{bigIntFromString(t, "1048576"), mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8), int64(8)},
			exit:  0,
		},
		{
			name:  "dictsetget_missing",
			code:  codeFromOpcodes(t, 0xF41A),
			stack: []any{cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().MustBeginParse(), mustSliceKey(t, 0x01, 8), nil, int64(8)},
			exit:  0,
		},
		{
			name:  "dictdelgetref_hit",
			code:  codeFromOpcodes(t, 0xF463),
			stack: []any{mustSliceKey(t, 0x10, 8), refDict.AsCell(), int64(8)},
			exit:  0,
		},
		{
			name:  "dictgetoptref_miss",
			code:  codeFromOpcodes(t, 0xF469),
			stack: []any{mustSliceKey(t, 0x99, 8), mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8), int64(8)},
			exit:  0,
		},
		{
			name:  "dictsetgetoptref_insert_nil_root",
			code:  codeFromOpcodes(t, 0xF46D),
			stack: []any{setGetOptRefValue, mustSliceKey(t, 0x77, 8), nil, int64(8)},
			exit:  0,
		},
		{
			name:  "dictusetgetoptref_negative_key_range",
			code:  codeFromOpcodes(t, 0xF46F),
			stack: []any{setGetOptRefValue, big.NewInt(-1), nil, int64(8)},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name:  "dictremmaxref",
			code:  codeFromOpcodes(t, 0xF49B),
			stack: []any{buildMinMaxRefDict(t).AsCell(), int64(8)},
			exit:  0,
		},
		{
			name:  "pfxdictgetq_miss",
			code:  codeFromOpcodes(t, 0xF4A8),
			stack: []any{mustSliceKey(t, 0b0111, 4), prefixDict, int64(4)},
			exit:  0,
		},
		{
			name:  "pfxdictget_hit",
			code:  codeFromOpcodes(t, 0xF4A9),
			stack: []any{mustSliceKey(t, 0b1011, 4), prefixDict, int64(4)},
			exit:  0,
		},
		{
			name:  "dictigetjmpz_hit",
			code:  codeFromOpcodes(t, 0xF4BC),
			stack: []any{bigIntFromString(t, "42"), jumpContDict.AsCell(), int64(8)},
			exit:  0,
		},
		{
			name:  "dictugetjmpz_miss_keeps_key",
			code:  codeFromOpcodes(t, 0xF4BD),
			stack: []any{bigIntFromString(t, "153"), jumpContDict.AsCell(), int64(8)},
			exit:  0,
		},
		{
			name:  "pfxdictswitch_hit",
			code:  codeFromBuilders(t, dictop.PFXDICTSWITCH(switchDict, 4).Serialize()),
			stack: []any{mustSliceKey(t, 0b1011, 4)},
			exit:  0,
		},
		{
			name:  "dictget_short_key_underflow",
			code:  codeFromOpcodes(t, 0xF40A),
			stack: []any{mustSliceKey(t, 0x1, 4), mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8), int64(8)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name:  "pfxdictget_miss_underflow",
			code:  codeFromOpcodes(t, 0xF4A9),
			stack: []any{mustSliceKey(t, 0b0111, 4), prefixDict, int64(4)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name:  "dictuset_negative_key",
			code:  codeFromOpcodes(t, 0xF416),
			stack: []any{cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().MustBeginParse(), bigIntFromString(t, "-1"), nil, int64(8)},
			exit:  vmerr.CodeRangeCheck,
		},
	}

}

func runDictParityCase(t *testing.T, tt dictOpsParityCase) {
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

	c7 := tt.c7
	goRes, err := runGoCrossCode(code, cell.BeginCell().EndCell(), c7, goStack)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refRes, err := runReferenceCrossCode(code, cell.BeginCell().EndCell(), c7, refStack)
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

func TestTVMCrossEmulatorDictOpsVersionedPrefixUnderflow(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := dictVersionedPrefixUnderflowCases(t)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_DICTOPS_VERSION_AUDIT")
	for _, tt := range tests {
		for _, version := range versions {
			t.Run(fmt.Sprintf("%s_v%d", tt.name, version), func(t *testing.T) {
				runDictVersionedParityCase(t, tt.code, tt.stack, version, int32(vmerr.CodeStackUnderflow))
			})
		}
	}
}

func FuzzTVMCrossEmulatorDictOpsVersionedPrefixUnderflow(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%dictVersionedPrefixUnderflowCaseCount))
	}
	for i := 0; i < dictVersionedPrefixUnderflowCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := dictVersionedPrefixUnderflowCases(t)
		if len(tests) != dictVersionedPrefixUnderflowCaseCount {
			t.Fatalf("dict prefix-underflow case count = %d, want %d", len(tests), dictVersionedPrefixUnderflowCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runDictVersionedParityCase(t, tt.code, tt.stack, version, int32(vmerr.CodeStackUnderflow))
	})
}

const dictVersionedPrefixUnderflowCaseCount = 8

type dictVersionedPrefixUnderflowCase struct {
	name  string
	code  *cell.Cell
	stack []any
}

func dictVersionedPrefixUnderflowCases(t *testing.T) []dictVersionedPrefixUnderflowCase {
	t.Helper()

	return []dictVersionedPrefixUnderflowCase{
		{name: "pfxdictdel_missing_root_and_key", code: codeFromOpcodes(t, 0xF473), stack: []any{int64(4)}},
		{name: "pfxdictdel_missing_key", code: codeFromOpcodes(t, 0xF473), stack: []any{nil, int64(4)}},
		{name: "pfxdictset_missing_value", code: codeFromOpcodes(t, 0xF470), stack: []any{mustSliceKey(t, 0b10, 2), nil, int64(4)}},
		{name: "pfxdictreplace_missing_value", code: codeFromOpcodes(t, 0xF471), stack: []any{mustSliceKey(t, 0b10, 2), nil, int64(4)}},
		{name: "pfxdictadd_missing_value", code: codeFromOpcodes(t, 0xF472), stack: []any{mustSliceKey(t, 0b10, 2), nil, int64(4)}},
		{name: "pfxdictget_missing_root_and_bits", code: codeFromOpcodes(t, 0xF4A9), stack: []any{mustSliceKey(t, 0b10, 2)}},
		{name: "pfxdictgetjmp_missing_root_and_bits", code: codeFromOpcodes(t, 0xF4AA), stack: []any{mustSliceKey(t, 0b10, 2)}},
		{name: "pfxdictgetexec_missing_root_and_bits", code: codeFromOpcodes(t, 0xF4AB), stack: []any{mustSliceKey(t, 0b10, 2)}},
	}
}

func buildMinMaxRefDict(t *testing.T) *cell.Dictionary {
	t.Helper()
	dict := cell.NewDict(8)
	minRef := cell.BeginCell().MustStoreUInt(0x1111, 16).EndCell()
	maxRef := cell.BeginCell().MustStoreUInt(0x2222, 16).EndCell()
	if _, err := dict.SetBuilderWithMode(mustKeyCell(t, 0x01, 8), cell.BeginCell().MustStoreRef(minRef), cell.DictSetModeSet); err != nil {
		t.Fatalf("failed to seed min ref: %v", err)
	}
	if _, err := dict.SetBuilderWithMode(mustKeyCell(t, 0xFE, 8), cell.BeginCell().MustStoreRef(maxRef), cell.DictSetModeSet); err != nil {
		t.Fatalf("failed to seed max ref: %v", err)
	}
	return dict
}

func runDictVersionedParityCase(t *testing.T, code *cell.Cell, stack []any, globalVersion int, wantExit int32) {
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
	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, wantExit)
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

func bigIntFromString(t *testing.T, src string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(src, 10)
	if !ok {
		t.Fatalf("invalid big int %q", src)
	}
	return v
}
