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

type dictEdgeCrossCase struct {
	name  string
	code  func(*testing.T) *cell.Cell
	stack func(*testing.T) []any
	exit  int32
}

func TestTVMCrossEmulatorDictOpsEdgeCandidates(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := dictEdgeAllCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDictEdgeCrossCase(t, prependRawMethodDrop(tt.code(t)), tt.stack(t), tt.exit)
		})
	}
}

func TestTVMCrossEmulatorDictOpsEdgeAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := dictEdgeAllCases()

	versions := crossEmulatorVersionAuditVersions(t, "TVM_DICTOPS_EDGE_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runDictVersionedParityCase(t, tt.code(t), tt.stack(t), version, tt.exit)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorDictOpsEdgeGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := dictEdgeAllCases()
	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(version%len(cases)))
	}
	for i := range cases {
		f.Add(uint8(MaxSupportedGlobalVersion), uint16(i))
	}
	f.Add(uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := dictEdgeAllCases()
		tt := tests[int(rawCase)%len(tests)]
		runDictVersionedParityCase(t, tt.code(t), tt.stack(t), version, tt.exit)
	})
}

func dictEdgeAllCases() []dictEdgeCrossCase {
	tests := []dictEdgeCrossCase{}
	tests = append(tests, dictEdgeWidthCases()...)
	tests = append(tests, dictEdgeBoundaryCases()...)
	tests = append(tests, dictEdgeMalformedRootCases()...)
	tests = append(tests, dictEdgePrefixCases()...)
	tests = append(tests, dictEdgeSubdictCases()...)
	tests = append(tests, dictEdgeNearCases()...)
	tests = append(tests, dictEdgeBuilderAndRefCases()...)
	return tests
}

func dictEdgeWidthCases() []dictEdgeCrossCase {
	return []dictEdgeCrossCase{
		{
			name: "width/dictget_zero_bit_hit",
			code: dictEdgeOpcode(0xF40A),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSliceKey(t, big.NewInt(0), 0), dictEdgeZeroRoot(t), int64(0)}
			},
			exit: 0,
		},
		{
			name: "width/dictuget_zero_bit_nonzero_miss",
			code: dictEdgeOpcode(0xF40E),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(1), dictEdgeZeroRoot(t), int64(0)}
			},
			exit: 0,
		},
		{
			name: "width/dictuset_zero_bit_update",
			code: dictEdgeOpcode(0xF416),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSlice(0xDD, 8), big.NewInt(0), dictEdgeZeroRoot(t), int64(0)}
			},
			exit: 0,
		},
		{
			name: "width/dictget_one_bit_left_hit",
			code: dictEdgeOpcode(0xF40A),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSliceKey(t, big.NewInt(0), 1), dictEdgeOneBitRoot(t), int64(1)}
			},
			exit: 0,
		},
		{
			name: "width/dictuget_one_bit_max_hit",
			code: dictEdgeOpcode(0xF40E),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(1), dictEdgeOneBitRoot(t), int64(1)}
			},
			exit: 0,
		},
		{
			name: "width/dictiget_one_bit_min_hit",
			code: dictEdgeOpcode(0xF40C),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictEdgeOneBitRoot(t), int64(1)}
			},
			exit: 0,
		},
		{
			name: "width/dictiset_one_bit_above_max_range",
			code: dictEdgeOpcode(0xF414),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSlice(0xAB, 8), big.NewInt(1), nil, int64(1)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "width/dictget_slice_257_hit",
			code: dictEdgeOpcode(0xF40A),
			stack: func(t *testing.T) []any {
				key := dictEdgeUnsignedMax(256)
				return []any{dictEdgeSliceKey(t, key, 257), dictEdge257Root(t), int64(257)}
			},
			exit: 0,
		},
		{
			name: "width/dictget_slice_257_all_ones_hit",
			code: dictEdgeOpcode(0xF40A),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSliceKey(t, big.NewInt(-1), 257), dictEdge257Root(t), int64(257)}
			},
			exit: 0,
		},
		{
			name: "width/dictiget_257_signed_min_hit",
			code: dictEdgeOpcode(0xF40C),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSignedMin(257), dictEdge257Root(t), int64(257)}
			},
			exit: 0,
		},
		{
			name: "width/dictiget_257_signed_max_hit",
			code: dictEdgeOpcode(0xF40C),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSignedMax(257), dictEdge257Root(t), int64(257)}
			},
			exit: 0,
		},
		{
			name: "width/dictuget_257_unsigned_high_hit",
			code: dictEdgeOpcode(0xF40E),
			stack: func(t *testing.T) []any {
				key := dictEdgeUnsignedMax(256)
				return []any{key, dictEdge257Root(t), int64(257)}
			},
			exit: 0,
		},
	}
}

func dictEdgeBoundaryCases() []dictEdgeCrossCase {
	return []dictEdgeCrossCase{
		{
			name: "boundary/dictiget_8_min_hit",
			code: dictEdgeOpcode(0xF40C),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-128), dictEdgeSigned8Root(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "boundary/dictiget_8_max_hit",
			code: dictEdgeOpcode(0xF40C),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(127), dictEdgeSigned8Root(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "boundary/dictiset_8_below_min_range",
			code: dictEdgeOpcode(0xF414),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSlice(0xA1, 8), big.NewInt(-129), nil, int64(8)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "boundary/dictiset_8_above_max_range",
			code: dictEdgeOpcode(0xF414),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSlice(0xA2, 8), big.NewInt(128), nil, int64(8)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "boundary/dictuget_8_zero_hit",
			code: dictEdgeOpcode(0xF40E),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0), dictEdgeUnsigned8Root(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "boundary/dictuget_8_max_hit",
			code: dictEdgeOpcode(0xF40E),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(255), dictEdgeUnsigned8Root(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "boundary/dictuset_8_above_max_range",
			code: dictEdgeOpcode(0xF416),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSlice(0xA3, 8), big.NewInt(256), nil, int64(8)}
			},
			exit: vmerr.CodeRangeCheck,
		},
	}
}

func dictEdgeMalformedRootCases() []dictEdgeCrossCase {
	return []dictEdgeCrossCase{
		{
			name: "root/dictget_malformed_long_label",
			code: dictEdgeOpcode(0xF40A),
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0, 8), dictEdgeMalformedLongLabelRoot(), int64(8)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "root/dictset_malformed_long_label",
			code: dictEdgeOpcode(0xF412),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSlice(0xB0, 8), mustSliceKey(t, 0, 8), dictEdgeMalformedLongLabelRoot(), int64(8)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "root/dictget_library_root",
			code: dictEdgeOpcode(0xF40A),
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0, 8), dictEdgeLibraryCell(t), int64(8)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "root/pfxdictgetq_library_root",
			code: dictEdgeOpcode(0xF4A8),
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b10, 2), dictEdgeLibraryCell(t), int64(4)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "root/near_getnext_malformed_long_label",
			code: dictEdgeOpcode(0xF474),
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0, 8), dictEdgeMalformedLongLabelRoot(), int64(8)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
	}
}

func dictEdgePrefixCases() []dictEdgeCrossCase {
	return []dictEdgeCrossCase{
		{
			name: "prefix/pfxdictgetq_empty_prefix_hit",
			code: dictEdgeOpcode(0xF4A8),
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4), dictEdgeEmptyPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictdel_empty_prefix_hit",
			code: dictEdgeOpcode(0xF473),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSliceKey(t, big.NewInt(0), 0), dictEdgeEmptyPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictset_key_too_long",
			code: dictEdgeOpcode(0xF470),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSlice(0xC, 4), mustSliceKey(t, 0b10101, 5), nil, int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictgetq_key_too_long",
			code: dictEdgeOpcode(0xF4A8),
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b10101, 5), dictEdgeEmptyPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictget_nil_root_underflow",
			code: dictEdgeOpcode(0xF4A9),
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4), nil, int64(4)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "prefix/pfxdictgetexec_nil_root_underflow",
			code: dictEdgeOpcode(0xF4AB),
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4), nil, int64(4)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "prefix/pfxdictswitch_empty_prefix_hit",
			code: func(t *testing.T) *cell.Cell {
				return codeFromBuilders(t, dictop.PFXDICTSWITCH(dictEdgeEmptyPrefixContRoot(t, 11), 4).Serialize())
			},
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4)}
			},
			exit: 0,
		},
	}
}

func dictEdgeSubdictCases() []dictEdgeCrossCase {
	return []dictEdgeCrossCase{
		{
			name: "subdict/slice_zero_prefix_hit",
			code: dictEdgeOpcode(0xF4B1),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSliceKey(t, big.NewInt(0), 0), int64(0), dictEdgeSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/slice_zero_prefix_remove_hit",
			code: dictEdgeOpcode(0xF4B5),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSliceKey(t, big.NewInt(0), 0), int64(0), dictEdgeSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/signed_prefix_value_underflow",
			code: dictEdgeOpcode(0xF4B2),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(1), int64(1), dictEdgeSigned8Root(t), int64(8)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "subdict/unsigned_257_prefix_bits_range",
			code: dictEdgeOpcode(0xF4B3),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0), int64(257), nil, int64(257)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "subdict/signed_257_prefix_hit",
			code: dictEdgeOpcode(0xF4B2),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), int64(257), dictEdgeSigned257MinusOneRoot(t), int64(257)}
			},
			exit: 0,
		},
		{
			name: "subdict/slice_key_len_zero_hit",
			code: dictEdgeOpcode(0xF4B1),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSliceKey(t, big.NewInt(0), 0), int64(0), dictEdgeZeroRoot(t), int64(0)}
			},
			exit: 0,
		},
	}
}

func dictEdgeNearCases() []dictEdgeCrossCase {
	return []dictEdgeCrossCase{
		{
			name: "near/getnexteq_zero_width_hit",
			code: dictEdgeOpcode(0xF475),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSliceKey(t, big.NewInt(0), 0), dictEdgeZeroRoot(t), int64(0)}
			},
			exit: 0,
		},
		{
			name: "near/getnext_zero_width_miss",
			code: dictEdgeOpcode(0xF474),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeSliceKey(t, big.NewInt(0), 0), dictEdgeZeroRoot(t), int64(0)}
			},
			exit: 0,
		},
		{
			name: "near/igetpreveq_zero_width_hit",
			code: dictEdgeOpcode(0xF47B),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0), dictEdgeZeroRoot(t), int64(0)}
			},
			exit: 0,
		},
		{
			name: "near/ugetnexteq_zero_width_hit",
			code: dictEdgeOpcode(0xF47D),
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0), dictEdgeZeroRoot(t), int64(0)}
			},
			exit: 0,
		},
		{
			name: "near/getnexteq_malformed_long_label",
			code: dictEdgeOpcode(0xF475),
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0, 8), dictEdgeMalformedLongLabelRoot(), int64(8)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
	}
}

func dictEdgeBuilderAndRefCases() []dictEdgeCrossCase {
	return []dictEdgeCrossCase{
		{
			name: "builder/iaddgetb_missing_existing_root",
			code: dictEdgeOpcode(0xF456),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeBuilder(0xB3, 8), big.NewInt(-3), dictEdgeSigned8Root(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/iaddgetb_existing",
			code: dictEdgeOpcode(0xF456),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeBuilder(0xB4, 8), big.NewInt(-1), dictEdgeSigned8Root(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "ref/setget_ref_update_existing",
			code: dictEdgeOpcode(0xF41B),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeRefValue(0x3333, 16), mustSliceKey(t, 0x10, 8), dictEdgeRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "ref/replaceget_ref_miss",
			code: dictEdgeOpcode(0xF42B),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeRefValue(0x4444, 16), mustSliceKey(t, 0x20, 8), dictEdgeRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "ref/iaddgetref_missing_existing_root",
			code: dictEdgeOpcode(0xF43D),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeRefValue(0x5555, 16), big.NewInt(-1), dictEdgeRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "ref/uaddgetref_existing",
			code: dictEdgeOpcode(0xF43F),
			stack: func(t *testing.T) []any {
				return []any{dictEdgeRefValue(0x6666, 16), big.NewInt(0xFE), dictEdgeRefRoot(t), int64(8)}
			},
			exit: 0,
		},
	}
}

func runDictEdgeCrossCase(t *testing.T, code *cell.Cell, stackValues []any, exit int32) {
	t.Helper()

	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
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

	if goRes.exitCode != exit || refRes.exitCode != exit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, exit)
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

func dictEdgeOpcode(op uint16) func(*testing.T) *cell.Cell {
	return func(t *testing.T) *cell.Cell {
		t.Helper()
		return codeFromOpcodes(t, op)
	}
}

type dictEdgeEntry struct {
	key       *cell.Cell
	value     uint64
	valueBits uint
}

func dictEdgePlainRoot(t *testing.T, keyBits uint, entries ...dictEdgeEntry) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(keyBits)
	for _, entry := range entries {
		value := cell.BeginCell().MustStoreUInt(entry.value, entry.valueBits).EndCell()
		if _, err := dict.SetWithMode(entry.key, value, cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to seed edge dict: %v", err)
		}
	}
	return dict.AsCell()
}

func dictEdgeZeroRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return dictEdgePlainRoot(t, 0, dictEdgeEntry{
		key:       dictEdgeKey(t, big.NewInt(0), 0),
		value:     0xAA,
		valueBits: 8,
	})
}

func dictEdgeOneBitRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return dictEdgePlainRoot(t, 1,
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(0), 1), value: 0x10, valueBits: 8},
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(1), 1), value: 0x11, valueBits: 8},
	)
}

func dictEdge257Root(t *testing.T) *cell.Cell {
	t.Helper()
	return dictEdgePlainRoot(t, 257,
		dictEdgeEntry{key: dictEdgeKey(t, dictEdgeSignedMin(257), 257), value: 0x80, valueBits: 8},
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(-1), 257), value: 0xFF, valueBits: 8},
		dictEdgeEntry{key: dictEdgeKey(t, dictEdgeSignedMax(257), 257), value: 0x7F, valueBits: 8},
	)
}

func dictEdgeSigned257MinusOneRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return dictEdgePlainRoot(t, 257, dictEdgeEntry{
		key:       dictEdgeKey(t, big.NewInt(-1), 257),
		value:     0xFF,
		valueBits: 8,
	})
}

func dictEdgeSigned8Root(t *testing.T) *cell.Cell {
	t.Helper()
	return dictEdgePlainRoot(t, 8,
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(-128), 8), value: 0x80, valueBits: 8},
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(-1), 8), value: 0xFF, valueBits: 8},
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(127), 8), value: 0x7F, valueBits: 8},
	)
}

func dictEdgeUnsigned8Root(t *testing.T) *cell.Cell {
	t.Helper()
	return dictEdgePlainRoot(t, 8,
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(0), 8), value: 0x00, valueBits: 8},
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(255), 8), value: 0xFF, valueBits: 8},
	)
}

func dictEdgeSubdictRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return dictEdgePlainRoot(t, 8,
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(0x10), 8), value: 0x10, valueBits: 8},
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(0xA1), 8), value: 0xA1, valueBits: 8},
		dictEdgeEntry{key: dictEdgeKey(t, big.NewInt(0xF0), 8), value: 0xF0, valueBits: 8},
	)
}

func dictEdgeRefRoot(t *testing.T) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(8)
	for key, value := range map[uint64]*cell.Cell{
		0x10: dictEdgeRefValue(0x1111, 16),
		0xFE: dictEdgeRefValue(0x2222, 16),
	} {
		if _, err := dict.SetBuilderWithMode(mustKeyCell(t, key, 8), cell.BeginCell().MustStoreRef(value), cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to seed edge ref dict: %v", err)
		}
	}
	return dict.AsCell()
}

func dictEdgeKey(t *testing.T, value *big.Int, bits uint) *cell.Cell {
	t.Helper()
	return cell.BeginCell().MustStoreBigInt(value, bits).EndCell()
}

func dictEdgeSliceKey(t *testing.T, value *big.Int, bits uint) *cell.Slice {
	t.Helper()
	return dictEdgeKey(t, value, bits).MustBeginParse()
}

func dictEdgeSlice(value uint64, bits uint) *cell.Slice {
	return cell.BeginCell().MustStoreUInt(value, bits).EndCell().MustBeginParse()
}

func dictEdgeBuilder(value uint64, bits uint) *cell.Builder {
	return cell.BeginCell().MustStoreUInt(value, bits)
}

func dictEdgeRefValue(value uint64, bits uint) *cell.Cell {
	return cell.BeginCell().MustStoreUInt(value, bits).EndCell()
}

func dictEdgeMalformedLongLabelRoot() *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreUInt(9, 4).
		MustStoreUInt(0, 9).
		EndCell()
}

func dictEdgeLibraryCell(t *testing.T) *cell.Cell {
	t.Helper()

	root, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(make([]byte, 32), 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to build library cell: %v", err)
	}
	return root
}

func dictEdgeEmptyPrefixRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
		0: {bits: 0, value: dictEdgeRefValue(0xE, 4)},
	})
}

func dictEdgeEmptyPrefixContRoot(t *testing.T, value int64) *cell.Cell {
	t.Helper()
	return mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
		0: {bits: 0, value: codeFromOpcodes(t, uint16(0x70|(value&0xF)))},
	})
}

func dictEdgeSignedMin(bits uint) *big.Int {
	if bits == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), bits-1))
}

func dictEdgeSignedMax(bits uint) *big.Int {
	if bits == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits-1), big.NewInt(1))
}

func dictEdgeUnsignedMax(bits uint) *big.Int {
	if bits == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1))
}
