//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorBLSValidationOrder(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	shortG1 := blsOrderByteSlice(10)
	shortG2 := blsOrderByteSlice(10)
	fullG1 := blsOrderByteSlice(48)
	fullG2 := blsOrderByteSlice(96)
	msg := blsOrderByteSlice(1)
	nonByteMsg := cell.BeginCell().MustStoreUInt(0, 1).ToSlice()

	tests := []struct {
		name  string
		op    *cell.Builder
		stack []any
		exit  int32
		gas   int64
	}{
		{
			name:  "verify_short_sig_before_bad_pub_type",
			op:    funcsop.BLS_VERIFY().Serialize(),
			stack: []any{int64(7), msg, shortG2},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   61_102,
		},
		{
			name:  "aggregate_all_sizes_before_curve_decode",
			op:    funcsop.BLS_AGGREGATE().Serialize(),
			stack: []any{shortG2, fullG2, int64(2)},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   6_152,
		},
		{
			name:  "fast_aggregate_non_byte_msg_charges_base_gas",
			op:    funcsop.BLS_FASTAGGREGATEVERIFY().Serialize(),
			stack: []any{int64(0), nonByteMsg, fullG2},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   58_102,
		},
		{
			name:  "fast_aggregate_bad_pub_type_before_short_sig",
			op:    funcsop.BLS_FASTAGGREGATEVERIFY().Serialize(),
			stack: []any{int64(7), int64(1), msg, shortG2},
			exit:  int32(vmerr.CodeTypeCheck),
			gas:   61_102,
		},
		{
			name:  "aggregate_verify_bad_pub_type_before_short_sig",
			op:    funcsop.BLS_AGGREGATEVERIFY().Serialize(),
			stack: []any{int64(7), msg, int64(1), shortG2},
			exit:  int32(vmerr.CodeTypeCheck),
			gas:   61_102,
		},
		{
			name:  "g1_add_short_top_before_bad_bottom_type",
			op:    funcsop.BLS_G1_ADD().Serialize(),
			stack: []any{int64(7), shortG1},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   4_002,
		},
		{
			name:  "g1_sub_short_top_before_bad_bottom_type",
			op:    funcsop.BLS_G1_SUB().Serialize(),
			stack: []any{int64(7), shortG1},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   4_002,
		},
		{
			name:  "g1_multiexp_all_sizes_before_curve_decode",
			op:    funcsop.BLS_G1_MULTIEXP().Serialize(),
			stack: []any{shortG1, int64(1), fullG1, int64(1), int64(2)},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   17_147,
		},
		{
			name:  "g2_add_short_top_before_bad_bottom_type",
			op:    funcsop.BLS_G2_ADD().Serialize(),
			stack: []any{int64(7), shortG2},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   6_202,
		},
		{
			name:  "g2_sub_short_top_before_bad_bottom_type",
			op:    funcsop.BLS_G2_SUB().Serialize(),
			stack: []any{int64(7), shortG2},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   6_202,
		},
		{
			name:  "g2_multiexp_all_sizes_before_curve_decode",
			op:    funcsop.BLS_G2_MULTIEXP().Serialize(),
			stack: []any{shortG2, int64(1), fullG2, int64(1), int64(2)},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   44_470,
		},
		{
			name:  "pairing_short_g2_before_bad_g1_type",
			op:    funcsop.BLS_PAIRING().Serialize(),
			stack: []any{int64(7), shortG2, int64(1)},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   31_902,
		},
		{
			name:  "pairing_all_sizes_before_curve_decode",
			op:    funcsop.BLS_PAIRING().Serialize(),
			stack: []any{fullG1, shortG2, fullG1, fullG2, int64(2)},
			exit:  int32(vmerr.CodeCellUnderflow),
			gas:   43_702,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(codeFromBuilders(t, tt.op))
			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build Go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), tuple.Tuple{}, goStack, vm.MaxSupportedGlobalVersion)
			if err != nil {
				t.Fatalf("Go execution failed: %v", err)
			}
			refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, vm.MaxSupportedGlobalVersion))
			refRes, err := runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, *refCfg)
			if err != nil {
				t.Fatalf("reference execution failed: %v", err)
			}

			if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
				t.Fatalf("exit code: Go=%d reference=%d want=%d", goRes.exitCode, refRes.exitCode, tt.exit)
			}
			if goRes.gasUsed != tt.gas || refRes.gasUsed != tt.gas {
				t.Fatalf("gas used: Go=%d reference=%d want=%d", goRes.gasUsed, refRes.gasUsed, tt.gas)
			}

			goOut, err := normalizeStackCell(goRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize Go stack: %v", err)
			}
			refOut, err := normalizeStackCell(refRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize reference stack: %v", err)
			}
			if !bytes.Equal(goOut.Hash(), refOut.Hash()) {
				t.Fatalf("stack mismatch:\nGo=%s\nreference=%s", goOut.Dump(), refOut.Dump())
			}
			assertCrossSkippedGoStack(t, goOut, []any{int64(0)})
		})
	}
}

func blsOrderByteSlice(size int) *cell.Slice {
	return cell.BeginCell().MustStoreSlice(make([]byte, size), uint(size*8)).ToSlice()
}
