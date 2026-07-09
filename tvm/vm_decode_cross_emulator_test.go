//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorDecodeFailures(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := []struct {
		name string
		code *cell.Cell
		exit int32
	}{
		{
			name: "pushref_missing_ref",
			code: cell.BeginCell().MustStoreUInt(0x88, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "strefconst_missing_ref",
			code: cell.BeginCell().MustStoreUInt(0xCF20, 16).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_long_len31_invalid",
			code: cell.BeginCell().MustStoreUInt(0x82, 8).MustStoreUInt(31, 5).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_tiny_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0x7, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_8_truncated_value_invalid",
			code: cell.BeginCell().MustStoreUInt(0x80, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_16_truncated_value_invalid",
			code: cell.BeginCell().MustStoreUInt(0x81, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_long_truncated_value_invalid",
			code: cell.BeginCell().MustStoreUInt(0x82, 8).MustStoreUInt(0, 5).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_long_truncated_size_invalid",
			code: cell.BeginCell().MustStoreUInt(0x82, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "getparamlong_255_invalid",
			code: cell.BeginCell().MustStoreUInt(0xF881FF, 24).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "blkdrop2_i0_invalid",
			code: cell.BeginCell().MustStoreUInt(0x6C00, 16).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "throwany_reserved_6_invalid",
			code: cell.BeginCell().MustStoreUInt(0xF2F6, 16).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushctr_c6_invalid",
			code: cell.BeginCell().MustStoreUInt(0xED46, 16).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushslice_long_refs5_invalid",
			code: cell.BeginCell().MustStoreUInt(0x8D, 8).MustStoreUInt(5<<7, 10).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushslice_long_truncated_prefix_invalid",
			code: cell.BeginCell().MustStoreUInt(0x8D, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushslice_short_missing_payload_invalid",
			code: cell.BeginCell().MustStoreUInt(0x8B, 8).MustStoreUInt(0, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushrefcont_missing_ref_invalid",
			code: cell.BeginCell().MustStoreUInt(0x8A, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushcont_small_missing_payload_invalid",
			code: cell.BeginCell().MustStoreUInt(0x9, 4).MustStoreUInt(1, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushcont_big_missing_ref_invalid",
			code: cell.BeginCell().MustStoreUInt(0x47, 7).MustStoreUInt(1, 2).MustStoreUInt(0, 7).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "stsliceconst_missing_payload_invalid",
			code: cell.BeginCell().MustStoreUInt(0xCF80>>7, 9).MustStoreUInt(0, 5).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "sdbeginsconst_missing_payload_invalid",
			code: cell.BeginCell().MustStoreUInt(0xD728>>3, 13).MustStoreUInt(0, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "debugstr_missing_payload_invalid",
			code: cell.BeginCell().MustStoreUInt(0xFEF, 12).MustStoreUInt(0, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "dictpushconst_missing_ref_invalid",
			code: cell.BeginCell().MustStoreSlice([]byte{0xF4, 0xA4}, 13).MustStoreUInt(1, 1).MustStoreUInt(0, 10).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pfxdictswitch_missing_ref_invalid",
			code: cell.BeginCell().MustStoreSlice([]byte{0xF4, 0xAC}, 13).MustStoreUInt(1, 1).MustStoreUInt(0, 10).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "addint_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0xA6, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "getparam_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0xF82, 12).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "setcp_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0xFF, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "accept_family_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0xF8, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "tuple_family_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0x6F, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "stack_compound_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0x5, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "continuation_db_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0xDB, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "continuation_ed_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0xED, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "getparamlong_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0xF881, 16).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "push_short_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x2, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pop_short_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x3, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "xchg_long_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x10, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushlong_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x56, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "blkswap_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x55, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "blkpush_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x5F, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(tt.code)
			goStack, err := buildCrossStack()
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack()
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
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goRes.stack.Dump(), refRes.stack.Dump())
			}
		})
	}
}

func TestTVMCrossEmulatorDecodeFailuresAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := decodeVersionedCases(t, 0)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_DECODE_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runDecodeVersionedCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorDecodeFailuresGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%decodeVersionedCaseCount), uint16(version))
	}
	for i := 0; i < decodeVersionedCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i), uint16(0x20+i))
	}
	f.Add(uint8(255), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8, rawValue uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := decodeVersionedCases(t, rawValue)
		if len(tests) != decodeVersionedCaseCount {
			t.Fatalf("decode versioned case count = %d, want %d", len(tests), decodeVersionedCaseCount)
		}
		runDecodeVersionedCase(t, tests[int(rawCase)%len(tests)], version)
	})
}

type decodeVersionedCase struct {
	name string
	code *cell.Cell
	exit int32
}

const decodeVersionedCaseCount = 38

func decodeVersionedCases(t *testing.T, value uint16) []decodeVersionedCase {
	t.Helper()

	truncatedLongLen := uint64(value % 31)
	invalidLongRefs := uint64(5 + value%3)
	missingDictBits := uint64(value & 0x3ff)

	return []decodeVersionedCase{
		{
			name: "pushref_missing_ref",
			code: cell.BeginCell().MustStoreUInt(0x88, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "strefconst_missing_ref",
			code: cell.BeginCell().MustStoreUInt(0xCF20, 16).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_long_len31_invalid",
			code: cell.BeginCell().MustStoreUInt(0x82, 8).MustStoreUInt(31, 5).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_tiny_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0x7, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_8_truncated_value_invalid",
			code: cell.BeginCell().MustStoreUInt(0x80, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_16_truncated_value_invalid",
			code: cell.BeginCell().MustStoreUInt(0x81, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_long_truncated_value_invalid",
			code: cell.BeginCell().MustStoreUInt(0x82, 8).MustStoreUInt(truncatedLongLen, 5).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushint_long_truncated_size_invalid",
			code: cell.BeginCell().MustStoreUInt(0x82, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "getparamlong_255_invalid",
			code: cell.BeginCell().MustStoreUInt(0xF881FF, 24).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "blkdrop2_i0_invalid",
			code: cell.BeginCell().MustStoreUInt(0x6C00, 16).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "throwany_reserved_6_invalid",
			code: cell.BeginCell().MustStoreUInt(0xF2F6, 16).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushctr_c6_invalid",
			code: cell.BeginCell().MustStoreUInt(0xED46, 16).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushslice_long_refs_invalid",
			code: cell.BeginCell().MustStoreUInt(0x8D, 8).MustStoreUInt(invalidLongRefs<<7, 10).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushslice_long_truncated_prefix_invalid",
			code: cell.BeginCell().MustStoreUInt(0x8D, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushslice_short_missing_payload_invalid",
			code: cell.BeginCell().MustStoreUInt(0x8B, 8).MustStoreUInt(0, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushrefcont_missing_ref_invalid",
			code: cell.BeginCell().MustStoreUInt(0x8A, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushcont_small_missing_payload_invalid",
			code: cell.BeginCell().MustStoreUInt(0x9, 4).MustStoreUInt(1, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushcont_big_missing_ref_invalid",
			code: cell.BeginCell().MustStoreUInt(0x47, 7).MustStoreUInt(1, 2).MustStoreUInt(0, 7).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "stsliceconst_missing_payload_invalid",
			code: cell.BeginCell().MustStoreUInt(0xCF80>>7, 9).MustStoreUInt(0, 5).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "sdbeginsconst_missing_payload_invalid",
			code: cell.BeginCell().MustStoreUInt(0xD728>>3, 13).MustStoreUInt(0, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "debugstr_missing_payload_invalid",
			code: cell.BeginCell().MustStoreUInt(0xFEF, 12).MustStoreUInt(0, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "dictpushconst_missing_ref_invalid",
			code: cell.BeginCell().MustStoreSlice([]byte{0xF4, 0xA4}, 13).MustStoreUInt(1, 1).MustStoreUInt(missingDictBits, 10).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pfxdictswitch_missing_ref_invalid",
			code: cell.BeginCell().MustStoreSlice([]byte{0xF4, 0xAC}, 13).MustStoreUInt(1, 1).MustStoreUInt(missingDictBits, 10).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "addint_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0xA6, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "getparam_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0xF82, 12).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "setcp_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0xFF, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "accept_family_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0xF8, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "tuple_family_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0x6F, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "stack_compound_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0x5, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "continuation_db_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0xDB, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "continuation_ed_truncated_opcode_invalid",
			code: cell.BeginCell().MustStoreUInt(0xED, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "getparamlong_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0xF881, 16).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "push_short_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x2, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pop_short_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x3, 4).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "xchg_long_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x10, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "pushlong_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x56, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "blkswap_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x55, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
		{
			name: "blkpush_truncated_immediate_invalid",
			code: cell.BeginCell().MustStoreUInt(0x5F, 8).EndCell(),
			exit: vmerr.CodeInvalidOpcode,
		},
	}
}

func runDecodeVersionedCase(t *testing.T, tt decodeVersionedCase, globalVersion int) {
	t.Helper()

	code := prependRawMethodDrop(tt.code)
	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack()
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

	if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goRes.stack.Dump(), refRes.stack.Dump())
	}
}
