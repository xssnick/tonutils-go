//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	dictop "github.com/xssnick/tonutils-go/tvm/op/dict"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorDictOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	type testCase struct {
		name  string
		code  *cell.Cell
		stack []any
		exit  int32
		c7    tuple.Tuple
	}

	refDict := cell.NewDict(8)
	refValue := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	if _, err := refDict.SetBuilderWithMode(mustKeyCell(t, 0x10, 8), cell.BeginCell().MustStoreRef(refValue), cell.DictSetModeSet); err != nil {
		t.Fatalf("failed to seed ref dict: %v", err)
	}

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

	tests := []testCase{
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
			stack: []any{cell.BeginCell().EndCell().BeginParse()},
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
			stack: []any{cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse(), mustSliceKey(t, 0x01, 8), nil, int64(8)},
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
			stack: []any{cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse(), bigIntFromString(t, "-1"), nil, int64(8)},
			exit:  vmerr.CodeRangeCheck,
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
		})
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

func bigIntFromString(t *testing.T, src string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(src, 10)
	if !ok {
		t.Fatalf("invalid big int %q", src)
	}
	return v
}
