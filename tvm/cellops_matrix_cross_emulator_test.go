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
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type cellParityCase struct {
	name  string
	code  *cell.Cell
	stack []any
	exit  int32
}

func TestTVMCrossEmulatorCellOpsMatrix(t *testing.T) {
	tests := cellOpsMatrixCases(t)

	runCellParityCases(t, tests)
}

func cellOpsMatrixCases(t *testing.T) []cellParityCase {
	t.Helper()

	tests := []cellParityCase{
		{name: "newc", code: codeFromBuilders(t, cellsliceop.NEWC().Serialize()), exit: 0},
		{name: "ctos_empty_stack", code: codeFromBuilders(t, cellsliceop.CTOS().Serialize()), exit: vmerr.CodeStackUnderflow},
		{name: "ctos_typecheck", code: codeFromBuilders(t, cellsliceop.CTOS().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "endc_empty_stack", code: codeFromBuilders(t, cellsliceop.ENDC().Serialize()), exit: vmerr.CodeStackUnderflow},
		{name: "ends_empty_stack", code: codeFromBuilders(t, cellsliceop.ENDS().Serialize()), exit: vmerr.CodeStackUnderflow},
		{name: "ends_typecheck", code: codeFromBuilders(t, cellsliceop.ENDS().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "ends_empty", code: codeFromBuilders(t, cellsliceop.ENDS().Serialize()), stack: []any{matrixSlice(t, 0, 0)}, exit: 0},
		{name: "ends_bits_left", code: codeFromBuilders(t, cellsliceop.ENDS().Serialize()), stack: []any{matrixSlice(t, 1, 0)}, exit: vmerr.CodeCellUnderflow},
		{name: "ends_refs_left", code: codeFromBuilders(t, cellsliceop.ENDS().Serialize()), stack: []any{matrixSlice(t, 0, 1)}, exit: vmerr.CodeCellUnderflow},
		{name: "ldref_underflow_refs0", code: codeFromBuilders(t, cellsliceop.LDREF().Serialize()), stack: []any{matrixSlice(t, 8, 0)}, exit: vmerr.CodeCellUnderflow},
		{name: "ldref_typecheck", code: codeFromBuilders(t, cellsliceop.LDREF().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "ldrefrtos_underflow_refs0", code: codeFromBuilders(t, cellsliceop.LDREFRTOS().Serialize()), stack: []any{matrixSlice(t, 8, 0)}, exit: vmerr.CodeCellUnderflow},
		{name: "ldrefrtos_typecheck", code: codeFromBuilders(t, cellsliceop.LDREFRTOS().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "pldrefvar_range_refs5", code: codeFromBuilders(t, cellsliceop.PLDREFVAR().Serialize()), stack: []any{matrixSlice(t, 0, 4), int64(5)}, exit: vmerr.CodeRangeCheck},
		{name: "pldrefidx_underflow", code: codeFromBuilders(t, cellsliceop.PLDREFIDX(3).Serialize()), stack: []any{matrixSlice(t, 0, 3)}, exit: vmerr.CodeCellUnderflow},
		{name: "ldslicex_success", code: codeFromBuilders(t, cellsliceop.LDSLICEX().Serialize()), stack: []any{matrixSlice(t, 8, 0), int64(4)}, exit: 0},
		{name: "ldslicex_range_1024", code: codeFromBuilders(t, cellsliceop.LDSLICEXQ().Serialize()), stack: []any{matrixSlice(t, 1023, 0), int64(1024)}, exit: vmerr.CodeRangeCheck},
		{name: "ldslicexq_underflow", code: codeFromBuilders(t, cellsliceop.LDSLICEXQ().Serialize()), stack: []any{matrixSlice(t, 8, 0), int64(9)}, exit: 0},
		{name: "pldslicexq_underflow", code: codeFromBuilders(t, cellsliceop.PLDSLICEXQ().Serialize()), stack: []any{matrixSlice(t, 8, 0), int64(9)}, exit: 0},
		{name: "ldixq_underflow", code: codeFromBuilders(t, cellsliceop.LDIXQ().Serialize()), stack: []any{matrixSlice(t, 8, 0), int64(9)}, exit: 0},
		{name: "lduxq_underflow", code: codeFromBuilders(t, cellsliceop.LDUXQ().Serialize()), stack: []any{matrixSlice(t, 8, 0), int64(9)}, exit: 0},
		{name: "pldixq_underflow", code: codeFromBuilders(t, cellsliceop.PLDIXQ().Serialize()), stack: []any{matrixSlice(t, 8, 0), int64(9)}, exit: 0},
		{name: "plduxq_underflow", code: codeFromBuilders(t, cellsliceop.PLDUXQ().Serialize()), stack: []any{matrixSlice(t, 8, 0), int64(9)}, exit: 0},
		{name: "ldi_fixedq_underflow", code: codeFromBuilders(t, cellsliceop.LDIFIX(8, true, false, false).Serialize()), stack: []any{matrixSlice(t, 4, 0)}, exit: 0},
		{name: "pldi_fixedq_underflow", code: codeFromBuilders(t, cellsliceop.PLDIFIX(8, true, false, false).Serialize()), stack: []any{matrixSlice(t, 4, 0)}, exit: 0},
		{name: "ldslice_fixed_underflow", code: codeFromBuilders(t, cellsliceop.LDSLICEFIX(8, false, false).Serialize()), stack: []any{matrixSlice(t, 4, 0)}, exit: vmerr.CodeCellUnderflow},
		{name: "ldslice_8_underflow", code: codeFromBuilders(t, cellsliceop.LDSLICE(8).Serialize()), stack: []any{matrixSlice(t, 4, 0)}, exit: vmerr.CodeCellUnderflow},
		{name: "ldslice_8_typecheck", code: codeFromBuilders(t, cellsliceop.LDSLICE(8).Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "ldslice_fixedq_underflow", code: codeFromBuilders(t, cellsliceop.LDSLICEFIX(8, true, false).Serialize()), stack: []any{matrixSlice(t, 4, 0)}, exit: 0},
		{name: "pldslice_fixedq_underflow", code: codeFromBuilders(t, cellsliceop.PLDSLICEFIX(8, true, false).Serialize()), stack: []any{matrixSlice(t, 4, 0)}, exit: 0},
		{name: "ldslice_fixedq_success", code: codeFromBuilders(t, cellsliceop.LDSLICEFIX(4, true, false).Serialize()), stack: []any{matrixSlice(t, 8, 0)}, exit: 0},
		{name: "pldslice_fixedq_success", code: codeFromBuilders(t, cellsliceop.PLDSLICEFIX(4, true, true).Serialize()), stack: []any{matrixSlice(t, 8, 0)}, exit: 0},
		{name: "ldix_short_stack_bad_width_order", code: codeFromBuilders(t, cellsliceop.LDIX().Serialize()), stack: []any{int64(258)}, exit: vmerr.CodeStackUnderflow},
		{name: "ldslicex_short_stack_bad_width_order", code: codeFromBuilders(t, cellsliceop.LDSLICEX().Serialize()), stack: []any{int64(1024)}, exit: vmerr.CodeStackUnderflow},
		{name: "ldslicexq_short_stack_bad_width_order", code: codeFromBuilders(t, cellsliceop.LDSLICEXQ().Serialize()), stack: []any{int64(1024)}, exit: vmerr.CodeStackUnderflow},
		{name: "pldrefvar_short_stack_bad_idx_order", code: codeFromBuilders(t, cellsliceop.PLDREFVAR().Serialize()), stack: []any{int64(4)}, exit: vmerr.CodeStackUnderflow},
		{name: "bchkrefs_short_stack_bad_refs_order", code: codeFromBuilders(t, cellsliceop.BCHKREFS().Serialize()), stack: []any{int64(8)}, exit: vmerr.CodeStackUnderflow},
		{name: "splitq_short_stack_bad_refs_order", code: codeFromBuilders(t, cellsliceop.SPLITQ().Serialize()), stack: []any{int64(5)}, exit: vmerr.CodeStackUnderflow},
		{name: "split_refs_range_5", code: codeFromBuilders(t, cellsliceop.SPLIT().Serialize()), stack: []any{matrixSlice(t, 0, 0), int64(0), int64(5)}, exit: vmerr.CodeRangeCheck},
		{name: "split_bits_range_1024", code: codeFromBuilders(t, cellsliceop.SPLIT().Serialize()), stack: []any{matrixSlice(t, 0, 0), int64(1024), int64(0)}, exit: vmerr.CodeRangeCheck},
		{name: "ldsame_short_stack_bad_bit_order", code: codeFromBuilders(t, cellsliceop.LDSAME().Serialize()), stack: []any{int64(2)}, exit: vmerr.CodeStackUnderflow},
		{name: "ldsame_empty_stack", code: codeFromBuilders(t, cellsliceop.LDSAME().Serialize()), exit: vmerr.CodeStackUnderflow},
		{name: "ldones_empty_stack", code: codeFromBuilders(t, cellsliceop.LDONES().Serialize()), exit: vmerr.CodeStackUnderflow},
		{name: "ldsame_range_bit2", code: codeFromBuilders(t, cellsliceop.LDSAME().Serialize()), stack: []any{matrixSliceFromBits(t, "0011"), int64(2)}, exit: vmerr.CodeRangeCheck},
		{name: "ldzeroes_typecheck", code: codeFromBuilders(t, cellsliceop.LDZEROES().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "sdskipfirst_zero", code: codeFromBuilders(t, cellsliceop.SDSKIPFIRST().Serialize()), stack: []any{matrixSlice(t, 8, 0), int64(0)}, exit: 0},
		{name: "sdeq_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.SDEQ().Serialize()), stack: []any{cell.BeginCell()}, exit: vmerr.CodeStackUnderflow},
		{name: "sdeq_top_typecheck", code: codeFromBuilders(t, cellsliceop.SDEQ().Serialize()), stack: []any{matrixSlice(t, 1, 0), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "sdeq_bottom_typecheck", code: codeFromBuilders(t, cellsliceop.SDEQ().Serialize()), stack: []any{int64(0), matrixSlice(t, 1, 0)}, exit: vmerr.CodeTypeCheck},
		{name: "sdpfxrev_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.SDPFXREV().Serialize()), stack: []any{int64(1)}, exit: vmerr.CodeStackUnderflow},
		{name: "sdbeginsx_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.SDBEGINSX().Serialize()), stack: []any{int64(1)}, exit: vmerr.CodeStackUnderflow},
		{name: "stu_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.STU(1).Serialize()), stack: []any{int64(1)}, exit: vmerr.CodeStackUnderflow},
		{name: "stref_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.STREF().Serialize()), stack: []any{matrixCell(t, 0, 0)}, exit: vmerr.CodeStackUnderflow},
		{name: "stslice_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.STSLICE().Serialize()), stack: []any{matrixSlice(t, 1, 0)}, exit: vmerr.CodeStackUnderflow},
		{name: "stb_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.STB().Serialize()), stack: []any{int64(1)}, exit: vmerr.CodeStackUnderflow},
		{name: "endc_typecheck", code: codeFromBuilders(t, cellsliceop.ENDC().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stb_top_typecheck", code: codeFromBuilders(t, cellsliceop.STB().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stref_value_typecheck", code: codeFromBuilders(t, cellsliceop.STREF().Serialize()), stack: []any{cell.BeginCell(), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "stslice_value_typecheck", code: codeFromBuilders(t, cellsliceop.STSLICE().Serialize()), stack: []any{cell.BeginCell(), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "stule4_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.STULE4().Serialize()), stack: []any{int64(1)}, exit: vmerr.CodeStackUnderflow},
		{name: "ldile4_typecheck", code: codeFromBuilders(t, cellsliceop.LDILE4().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stule4_builder_typecheck", code: codeFromBuilders(t, cellsliceop.STULE4().Serialize()), stack: []any{int64(1), matrixCell(t, 0, 0)}, exit: vmerr.CodeTypeCheck},
		{name: "stile4_value_typecheck", code: codeFromBuilders(t, cellsliceop.STILE4().Serialize()), stack: []any{matrixCell(t, 0, 0), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "stile8_value_typecheck", code: codeFromBuilders(t, cellsliceop.STILE8().Serialize()), stack: []any{matrixCell(t, 0, 0), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "endcst_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.ENDCST().Serialize()), stack: []any{int64(1)}, exit: vmerr.CodeStackUnderflow},
		{name: "endxc_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.ENDXC().Serialize()), stack: []any{cell.BeginCell()}, exit: vmerr.CodeStackUnderflow},
		{name: "ldux_range_257", code: codeFromBuilders(t, cellsliceop.LDUX().Serialize()), stack: []any{matrixSlice(t, 257, 0), int64(257)}, exit: vmerr.CodeRangeCheck},
		{name: "ldix_range_258", code: codeFromBuilders(t, cellsliceop.LDIX().Serialize()), stack: []any{matrixSlice(t, 257, 0), int64(258)}, exit: vmerr.CodeRangeCheck},
		{name: "ldux_width_typecheck", code: codeFromBuilders(t, cellsliceop.LDUX().Serialize()), stack: []any{matrixSlice(t, 8, 0), matrixCell(t, 0, 0)}, exit: vmerr.CodeTypeCheck},
		{name: "ldix_slice_typecheck", code: codeFromBuilders(t, cellsliceop.LDIX().Serialize()), stack: []any{matrixCell(t, 0, 0), int64(8)}, exit: vmerr.CodeTypeCheck},
		{name: "pldix_underflow", code: codeFromBuilders(t, cellsliceop.PLDIX().Serialize()), stack: []any{matrixSlice(t, 4, 0), int64(5)}, exit: vmerr.CodeCellUnderflow},
		{name: "plduxq_underflow_preload", code: codeFromBuilders(t, cellsliceop.PLDUXQ().Serialize()), stack: []any{matrixSlice(t, 4, 0), int64(5)}, exit: 0},
		{name: "plduz_short_zero_extend", code: codeFromBuilders(t, cellsliceop.PLDUZ(32).Serialize()), stack: []any{cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().MustBeginParse()}, exit: 0},
		{name: "plduz_empty_zero_extend", code: codeFromBuilders(t, cellsliceop.PLDUZ(256).Serialize()), stack: []any{matrixSlice(t, 0, 0)}, exit: 0},
		{name: "plduz_typecheck", code: codeFromBuilders(t, cellsliceop.PLDUZ(32).Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stu_builder_full_bits", code: codeFromBuilders(t, cellsliceop.STU(1).Serialize()), stack: []any{int64(1), matrixBuilder(t, 1023, 0)}, exit: vmerr.CodeCellOverflow},
		{name: "stu_overflow_precedes_range", code: codeFromBuilders(t, cellsliceop.STU(1).Serialize()), stack: []any{int64(2), matrixBuilder(t, 1023, 0)}, exit: vmerr.CodeCellOverflow},
		{name: "sti_overflow_precedes_range", code: codeFromBuilders(t, cellsliceop.STI(1).Serialize()), stack: []any{int64(1), matrixBuilder(t, 1023, 0)}, exit: vmerr.CodeCellOverflow},
		{name: "stref_builder_full_refs", code: codeFromBuilders(t, cellsliceop.STREF().Serialize()), stack: []any{matrixCell(t, 0, 0), matrixBuilder(t, 0, 4)}, exit: vmerr.CodeCellOverflow},
		{name: "stb_builder_full_bits", code: codeFromBuilders(t, cellsliceop.STB().Serialize()), stack: []any{matrixBuilder(t, 1, 0), matrixBuilder(t, 1023, 0)}, exit: vmerr.CodeCellOverflow},
		{name: "stslice_builder_full_bits", code: codeFromBuilders(t, cellsliceop.STSLICE().Serialize()), stack: []any{matrixSlice(t, 1, 0), matrixBuilder(t, 1023, 0)}, exit: vmerr.CodeCellOverflow},
		{name: "stbref_top_typecheck", code: codeFromBuilders(t, cellsliceop.STBREF().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stbrefr_bottom_typecheck", code: codeFromBuilders(t, cellsliceop.STBREFR().Serialize()), stack: []any{int64(0), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "stbr_top_typecheck", code: codeFromBuilders(t, cellsliceop.STBR().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stslicer_top_typecheck", code: codeFromBuilders(t, cellsliceop.STSLICER().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stbr_builder_full_bits", code: codeFromBuilders(t, cellsliceop.STBR().Serialize()), stack: []any{matrixBuilder(t, 1023, 0), matrixBuilder(t, 1, 0)}, exit: vmerr.CodeCellOverflow},
		{name: "stslicer_builder_full_bits", code: codeFromBuilders(t, cellsliceop.STSLICER().Serialize()), stack: []any{matrixBuilder(t, 1023, 0), matrixSlice(t, 1, 0)}, exit: vmerr.CodeCellOverflow},
		{name: "strefq_success", code: codeFromBuilders(t, cellsliceop.STREFQ().Serialize()), stack: []any{matrixCell(t, 0, 0), cell.BeginCell()}, exit: 0},
		{name: "strefq_builder_full_refs", code: codeFromBuilders(t, cellsliceop.STREFQ().Serialize()), stack: []any{matrixCell(t, 0, 0), matrixBuilder(t, 0, 4)}, exit: 0},
		{name: "strefq_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.STREFQ().Serialize()), stack: []any{matrixCell(t, 0, 0)}, exit: vmerr.CodeStackUnderflow},
		{name: "strefq_top_typecheck", code: codeFromBuilders(t, cellsliceop.STREFQ().Serialize()), stack: []any{matrixCell(t, 0, 0), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "strefq_bottom_typecheck", code: codeFromBuilders(t, cellsliceop.STREFQ().Serialize()), stack: []any{cell.BeginCell(), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "stbrefq_success", code: codeFromBuilders(t, cellsliceop.STBREFQ().Serialize()), stack: []any{matrixBuilder(t, 8, 0), cell.BeginCell()}, exit: 0},
		{name: "stbrefq_top_typecheck", code: codeFromBuilders(t, cellsliceop.STBREFQ().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stbrefq_bottom_typecheck", code: codeFromBuilders(t, cellsliceop.STBREFQ().Serialize()), stack: []any{matrixCell(t, 0, 0), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "stsliceq_builder_full_bits", code: codeFromBuilders(t, cellsliceop.STSLICEQ().Serialize()), stack: []any{matrixSlice(t, 1, 0), matrixBuilder(t, 1023, 0)}, exit: 0},
		{name: "stsliceq_builder_full_refs", code: codeFromBuilders(t, cellsliceop.STSLICEQ().Serialize()), stack: []any{matrixSlice(t, 0, 1), matrixBuilder(t, 0, 4)}, exit: 0},
		{name: "stsliceq_top_typecheck", code: codeFromBuilders(t, cellsliceop.STSLICEQ().Serialize()), stack: []any{matrixSlice(t, 1, 0), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stsliceq_bottom_typecheck", code: codeFromBuilders(t, cellsliceop.STSLICEQ().Serialize()), stack: []any{matrixCell(t, 0, 0), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "stbq_top_typecheck", code: codeFromBuilders(t, cellsliceop.STBQ().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stbq_bottom_typecheck", code: codeFromBuilders(t, cellsliceop.STBQ().Serialize()), stack: []any{matrixCell(t, 0, 0), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "stbrefrq_success", code: codeFromBuilders(t, cellsliceop.STBREFRQ().Serialize()), stack: []any{cell.BeginCell(), matrixBuilder(t, 8, 0)}, exit: 0},
		{name: "stbrefrq_top_typecheck", code: codeFromBuilders(t, cellsliceop.STBREFRQ().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stbrefrq_bottom_typecheck", code: codeFromBuilders(t, cellsliceop.STBREFRQ().Serialize()), stack: []any{matrixCell(t, 0, 0), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "strefrq_top_typecheck", code: codeFromBuilders(t, cellsliceop.STREFRQ().Serialize()), stack: []any{cell.BeginCell(), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "strefrq_bottom_typecheck", code: codeFromBuilders(t, cellsliceop.STREFRQ().Serialize()), stack: []any{cell.BeginCell().EndCell(), matrixCell(t, 0, 0)}, exit: vmerr.CodeTypeCheck},
		{name: "stslicerq_success", code: codeFromBuilders(t, cellsliceop.STSLICERQ().Serialize()), stack: []any{cell.BeginCell(), matrixSlice(t, 8, 0)}, exit: 0},
		{name: "stslicerq_top_typecheck", code: codeFromBuilders(t, cellsliceop.STSLICERQ().Serialize()), stack: []any{cell.BeginCell(), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "stslicerq_bottom_typecheck", code: codeFromBuilders(t, cellsliceop.STSLICERQ().Serialize()), stack: []any{matrixCell(t, 0, 0), matrixSlice(t, 1, 0)}, exit: vmerr.CodeTypeCheck},
		{name: "stbrq_success", code: codeFromBuilders(t, cellsliceop.STBRQ().Serialize()), stack: []any{cell.BeginCell(), matrixBuilder(t, 8, 0)}, exit: 0},
		{name: "stbrq_top_typecheck", code: codeFromBuilders(t, cellsliceop.STBRQ().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stbrq_bottom_typecheck", code: codeFromBuilders(t, cellsliceop.STBRQ().Serialize()), stack: []any{matrixCell(t, 0, 0), cell.BeginCell()}, exit: vmerr.CodeTypeCheck},
		{name: "stref_cf10_alias", code: cell.BeginCell().MustStoreUInt(0xCF10, 16).EndCell(), stack: []any{matrixCell(t, 0, 0), cell.BeginCell()}, exit: 0},
		{name: "stslice_cf12_alias", code: cell.BeginCell().MustStoreUInt(0xCF12, 16).EndCell(), stack: []any{matrixSlice(t, 4, 0), cell.BeginCell()}, exit: 0},
		{name: "ldgrams_zero", code: codeFromBuilders(t, cellsliceop.LDGRAMS().Serialize()), stack: []any{cell.BeginCell().MustStoreCoins(0).EndCell().MustBeginParse()}, exit: 0},
		{name: "ldgrams_large", code: codeFromBuilders(t, cellsliceop.LDGRAMS().Serialize()), stack: []any{cell.BeginCell().MustStoreBigCoins(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 120), big.NewInt(1))).EndCell().MustBeginParse()}, exit: 0},
		{name: "ldgrams_typecheck", code: codeFromBuilders(t, cellsliceop.LDGRAMS().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "stgrams_zero", code: codeFromBuilders(t, cellsliceop.STGRAMS().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: 0},
		{name: "stgrams_one", code: codeFromBuilders(t, cellsliceop.STGRAMS().Serialize()), stack: []any{cell.BeginCell(), int64(1)}, exit: 0},
		{name: "stgrams_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.STGRAMS().Serialize()), stack: []any{int64(1)}, exit: vmerr.CodeStackUnderflow},
		{name: "stgrams_builder_typecheck", code: codeFromBuilders(t, cellsliceop.STGRAMS().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(1)}, exit: vmerr.CodeTypeCheck},
		{name: "stgrams_overflow_value", code: codeFromBuilders(t, cellsliceop.STGRAMS().Serialize()), stack: []any{cell.BeginCell(), new(big.Int).Lsh(big.NewInt(1), 120)}, exit: vmerr.CodeRangeCheck},
		{name: "hashcu_empty_stack", code: codeFromBuilders(t, cellsliceop.HASHCU().Serialize()), exit: vmerr.CodeStackUnderflow},
		{name: "hashcu_typecheck", code: codeFromBuilders(t, cellsliceop.HASHCU().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "hashcu_empty", code: codeFromBuilders(t, cellsliceop.HASHCU().Serialize()), stack: []any{matrixCell(t, 0, 0)}, exit: 0},
		{name: "hashsu_empty_stack", code: codeFromBuilders(t, cellsliceop.HASHSU().Serialize()), exit: vmerr.CodeStackUnderflow},
		{name: "hashsu_typecheck", code: codeFromBuilders(t, cellsliceop.HASHSU().Serialize()), stack: []any{matrixCell(t, 0, 0)}, exit: vmerr.CodeTypeCheck},
		{name: "hashsu_empty", code: codeFromBuilders(t, cellsliceop.HASHSU().Serialize()), stack: []any{matrixSlice(t, 0, 0)}, exit: 0},
		{name: "sdepth_empty", code: codeFromBuilders(t, cellsliceop.SDEPTH().Serialize()), stack: []any{matrixSlice(t, 0, 0)}, exit: 0},
		{name: "sdepth_typecheck", code: codeFromBuilders(t, cellsliceop.SDEPTH().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "cdepth_empty_stack", code: codeFromBuilders(t, cellsliceop.CDEPTH().Serialize()), exit: vmerr.CodeStackUnderflow},
		{name: "cdepth_typecheck", code: codeFromBuilders(t, cellsliceop.CDEPTH().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "cdepth_nil", code: codeFromBuilders(t, cellsliceop.CDEPTH().Serialize()), stack: []any{nil}, exit: 0},
		{name: "clevel_typecheck", code: codeFromBuilders(t, cellsliceop.CLEVEL().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "clevelmask_typecheck", code: codeFromBuilders(t, cellsliceop.CLEVELMASK().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		{name: "schkbitrefsq_short_stack_bad_refs_order", code: codeFromBuilders(t, cellsliceop.SCHKBITREFSQ().Serialize()), stack: []any{matrixSlice(t, 8, 1), int64(8)}, exit: vmerr.CodeStackUnderflow},
		{name: "schkbitrefsq_top_typecheck", code: codeFromBuilders(t, cellsliceop.SCHKBITREFSQ().Serialize()), stack: []any{matrixSlice(t, 8, 1), int64(8), cell.BeginCell().EndCell()}, exit: vmerr.CodeTypeCheck},
		{name: "schkbitrefsq_slice_typecheck", code: codeFromBuilders(t, cellsliceop.SCHKBITREFSQ().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(8), int64(1)}, exit: vmerr.CodeTypeCheck},
		{name: "chashix_range_4", code: codeFromBuilders(t, cellsliceop.CHASHIX().Serialize()), stack: []any{matrixDeepCell(t), int64(4)}, exit: vmerr.CodeRangeCheck},
		{name: "cdepthix_range_4", code: codeFromBuilders(t, cellsliceop.CDEPTHIX().Serialize()), stack: []any{matrixDeepCell(t), int64(4)}, exit: vmerr.CodeRangeCheck},
		{name: "chashix_missing_cell_after_valid_idx", code: codeFromBuilders(t, cellsliceop.CHASHIX().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeStackUnderflow},
		{name: "cdepthix_missing_cell_after_valid_idx", code: codeFromBuilders(t, cellsliceop.CDEPTHIX().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeStackUnderflow},
		{name: "chashix_range_precedes_missing_cell", code: codeFromBuilders(t, cellsliceop.CHASHIX().Serialize()), stack: []any{int64(4)}, exit: vmerr.CodeRangeCheck},
		{name: "cdepthix_range_precedes_missing_cell", code: codeFromBuilders(t, cellsliceop.CDEPTHIX().Serialize()), stack: []any{int64(4)}, exit: vmerr.CodeRangeCheck},
	}

	for _, bits := range []uint{0, 1, 8, 255, 256, 257, 1023} {
		tests = append(tests, cellParityCase{
			name:  fmt.Sprintf("ldslicex_bits_%d", bits),
			code:  codeFromBuilders(t, cellsliceop.PLDSLICEX().Serialize()),
			stack: []any{matrixSlice(t, bits, 0), int64(bits)},
			exit:  0,
		})
	}

	for _, bits := range []uint{0, 1, 8, 255, 256, 257} {
		tests = append(tests, cellParityCase{
			name:  fmt.Sprintf("ldix_bits_%d", bits),
			code:  codeFromBuilders(t, cellsliceop.LDIX().Serialize()),
			stack: []any{matrixSlice(t, bits, 0), int64(bits)},
			exit:  0,
		})
		if bits <= 256 {
			tests = append(tests, cellParityCase{
				name:  fmt.Sprintf("ldux_bits_%d", bits),
				code:  codeFromBuilders(t, cellsliceop.LDUX().Serialize()),
				stack: []any{matrixSlice(t, bits, 0), int64(bits)},
				exit:  0,
			})
		}
	}

	for _, bits := range []uint{0, 1, 8, 255, 256} {
		tests = append(tests,
			cellParityCase{name: fmt.Sprintf("pldix_bits_%d", bits), code: codeFromBuilders(t, cellsliceop.PLDIX().Serialize()), stack: []any{matrixSlice(t, bits, 0), int64(bits)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("pldux_bits_%d", bits), code: codeFromBuilders(t, cellsliceop.PLDUX().Serialize()), stack: []any{matrixSlice(t, bits, 0), int64(bits)}, exit: 0},
		)
	}

	for _, bits := range []uint{1, 8, 255, 256} {
		tests = append(tests,
			cellParityCase{name: fmt.Sprintf("ldi_fixed_%d", bits), code: codeFromBuilders(t, cellsliceop.LDI(bits).Serialize()), stack: []any{matrixSlice(t, bits, 0)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("ldu_fixed_%d", bits), code: codeFromBuilders(t, cellsliceop.LDU(bits).Serialize()), stack: []any{matrixSlice(t, bits, 0)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("pldu_fixed_%d", bits), code: codeFromBuilders(t, cellsliceop.PLDU(bits).Serialize()), stack: []any{matrixSlice(t, bits, 0)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("pldi_family_fixed_%d", bits), code: codeFromBuilders(t, cellsliceop.PLDIFIX(bits, false, true, false).Serialize()), stack: []any{matrixSlice(t, bits, 0)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("ldslice_%d", bits), code: codeFromBuilders(t, cellsliceop.LDSLICE(bits).Serialize()), stack: []any{matrixSlice(t, bits, 0)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("ldslice_fixed_%d", bits), code: codeFromBuilders(t, cellsliceop.LDSLICEFIX(bits, false, false).Serialize()), stack: []any{matrixSlice(t, bits, 0)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("pldslice_fixed_%d", bits), code: codeFromBuilders(t, cellsliceop.PLDSLICEFIX(bits, false, true).Serialize()), stack: []any{matrixSlice(t, bits, 0)}, exit: 0},
		)
	}

	for _, bits := range []uint{1, 8, 255, 256} {
		tests = append(tests,
			cellParityCase{name: fmt.Sprintf("sti_%d", bits), code: codeFromBuilders(t, cellsliceop.STI(bits).Serialize()), stack: []any{big.NewInt(0), cell.BeginCell()}, exit: 0},
			cellParityCase{name: fmt.Sprintf("stu_%d", bits), code: codeFromBuilders(t, cellsliceop.STU(bits).Serialize()), stack: []any{big.NewInt(0), cell.BeginCell()}, exit: 0},
		)
	}

	tests = append(tests,
		cellParityCase{name: "sti_1_positive_rangecheck", code: codeFromBuilders(t, cellsliceop.STI(1).Serialize()), stack: []any{int64(1), cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "sti_8_positive_rangecheck", code: codeFromBuilders(t, cellsliceop.STI(8).Serialize()), stack: []any{int64(128), cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "sti_256_positive_rangecheck", code: codeFromBuilders(t, cellsliceop.STI(256).Serialize()), stack: []any{new(big.Int).Lsh(big.NewInt(1), 255), cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "stix_ext_width_257", code: storeIntVarExtCode(0), stack: []any{big.NewInt(-1), cell.BeginCell(), int64(257)}, exit: 0},
		cellParityCase{name: "stux_ext_success", code: storeIntVarExtCode(1), stack: []any{int64(5), cell.BeginCell(), int64(3)}, exit: 0},
		cellParityCase{name: "stux_ext_width_rangecheck", code: storeIntVarExtCode(1), stack: []any{int64(0), cell.BeginCell(), int64(257)}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "stixrq_ext_range_fail", code: storeIntVarExtCode(6), stack: []any{cell.BeginCell(), int64(8), int64(3)}, exit: 0},
		cellParityCase{name: "stu_ext_fixed_reverse_success", code: storeIntFixedExtCode(3, 8), stack: []any{cell.BeginCell(), int64(255)}, exit: 0},
		cellParityCase{name: "stu_ext_fixed_negative_rangecheck", code: storeIntFixedExtCode(1, 8), stack: []any{int64(-1), cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "stiq_ext_fixed_range_fail", code: storeIntFixedExtCode(4, 8), stack: []any{int64(256), cell.BeginCell()}, exit: 0},
		cellParityCase{name: "stuq_ext_fixed_overflow_fail", code: storeIntFixedExtCode(5, 8), stack: []any{int64(1), matrixBuilder(t, 1016, 0)}, exit: 0},
		cellParityCase{name: "stixq_ext_overflow_precedes_range", code: storeIntVarExtCode(4), stack: []any{int64(2), matrixBuilder(t, 1023, 0), int64(1)}, exit: 0},
		cellParityCase{name: "stix_ext_overflow_precedes_range", code: storeIntVarExtCode(0), stack: []any{int64(2), matrixBuilder(t, 1023, 0), int64(1)}, exit: vmerr.CodeCellOverflow},
	)

	for _, refs := range []int{0, 1, 2, 3, 4} {
		tests = append(tests,
			cellParityCase{name: fmt.Sprintf("ctos_bits8_refs%d", refs), code: codeFromBuilders(t, cellsliceop.CTOS().Serialize()), stack: []any{matrixCell(t, 8, refs)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("ldref_refs%d", refs), code: codeFromBuilders(t, cellsliceop.LDREF().Serialize()), stack: []any{matrixSlice(t, 0, refs)}, exit: cellRefLoadExit(refs)},
			cellParityCase{name: fmt.Sprintf("endc_bits8_refs%d", refs), code: codeFromBuilders(t, cellsliceop.ENDC().Serialize()), stack: []any{matrixBuilder(t, 8, refs)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("hashcu_bits8_refs%d", refs), code: codeFromBuilders(t, cellsliceop.HASHCU().Serialize()), stack: []any{matrixCell(t, 8, refs)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("hashsu_bits8_refs%d", refs), code: codeFromBuilders(t, cellsliceop.HASHSU().Serialize()), stack: []any{matrixSlice(t, 8, refs)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("sdepth_bits8_refs%d", refs), code: codeFromBuilders(t, cellsliceop.SDEPTH().Serialize()), stack: []any{matrixSlice(t, 8, refs)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("bchkrefsq_refs%d", refs), code: codeFromBuilders(t, cellsliceop.BCHKREFSQ().Serialize()), stack: []any{matrixBuilder(t, 0, refs), int64(4 - refs)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("schkrefsq_refs%d", refs), code: codeFromBuilders(t, cellsliceop.SCHKREFSQ().Serialize()), stack: []any{matrixSlice(t, 0, refs), int64(refs + 1)}, exit: 0},
		)
	}
	tests = append(tests,
		cellParityCase{name: "ldi_fixed_empty_stack", code: codeFromBuilders(t, cellsliceop.LDI(8).Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "ldi_fixed_typecheck", code: codeFromBuilders(t, cellsliceop.LDI(8).Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "ldi_fixed_underflow", code: codeFromBuilders(t, cellsliceop.LDI(8).Serialize()), stack: []any{matrixSlice(t, 4, 0)}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "ldu_fixed_65_underflow", code: codeFromBuilders(t, cellsliceop.LDU(65).Serialize()), stack: []any{matrixSlice(t, 64, 0)}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "pldu_fixed_empty_stack", code: codeFromBuilders(t, cellsliceop.PLDU(8).Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "pldu_fixed_typecheck", code: codeFromBuilders(t, cellsliceop.PLDU(8).Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "pldu_fixed_underflow", code: codeFromBuilders(t, cellsliceop.PLDU(8).Serialize()), stack: []any{matrixSlice(t, 4, 0)}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "ldslicex_empty_stack", code: codeFromBuilders(t, cellsliceop.LDSLICEX().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "ldslicex_slice_typecheck", code: codeFromBuilders(t, cellsliceop.LDSLICEX().Serialize()), stack: []any{int64(0), int64(1)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "pldslicex_empty_stack", code: codeFromBuilders(t, cellsliceop.PLDSLICEX().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "ldslice_fixed_typecheck", code: codeFromBuilders(t, cellsliceop.LDSLICEFIX(8, false, false).Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "pldslice_fixed_typecheck", code: codeFromBuilders(t, cellsliceop.PLDSLICEFIX(8, false, true).Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
	)

	tests = append(tests,
		cellParityCase{name: "schkrefs_nonquiet_underflow", code: codeFromBuilders(t, cellsliceop.SCHKREFS().Serialize()), stack: []any{matrixSlice(t, 0, 1), int64(2)}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "schkrefs_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.SCHKREFS().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "schkrefs_top_typecheck", code: codeFromBuilders(t, cellsliceop.SCHKREFS().Serialize()), stack: []any{matrixSlice(t, 0, 1), cell.BeginCell().EndCell()}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "schkrefs_slice_typecheck", code: codeFromBuilders(t, cellsliceop.SCHKREFS().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "schkbitsq_short_stack_bad_order", code: codeFromBuilders(t, cellsliceop.SCHKBITSQ().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "schkrefsq_slice_typecheck", code: codeFromBuilders(t, cellsliceop.SCHKREFSQ().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "schkbitrefs_short_stack_bad_refs_order", code: codeFromBuilders(t, cellsliceop.SCHKBITREFS().Serialize()), stack: []any{matrixSlice(t, 8, 1), int64(8)}, exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "schkbitrefs_nonquiet_refs_underflow", code: codeFromBuilders(t, cellsliceop.SCHKBITREFS().Serialize()), stack: []any{matrixSlice(t, 8, 1), int64(8), int64(2)}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "pldrefvar_idx3_success", code: codeFromBuilders(t, cellsliceop.PLDREFVAR().Serialize()), stack: []any{matrixSlice(t, 0, 4), int64(3)}, exit: 0},
		cellParityCase{name: "pldrefidx_idx3_success", code: codeFromBuilders(t, cellsliceop.PLDREFIDX(3).Serialize()), stack: []any{matrixSlice(t, 0, 4)}, exit: 0},
	)

	deep := matrixDeepCell(t)
	for _, idx := range []int{0, 1, 2, 3} {
		tests = append(tests,
			cellParityCase{name: fmt.Sprintf("chashi_%d", idx), code: codeFromBuilders(t, cellsliceop.CHASHI(idx).Serialize()), stack: []any{deep}, exit: 0},
			cellParityCase{name: fmt.Sprintf("cdepthi_%d", idx), code: codeFromBuilders(t, cellsliceop.CDEPTHI(idx).Serialize()), stack: []any{deep}, exit: 0},
			cellParityCase{name: fmt.Sprintf("chashix_%d", idx), code: codeFromBuilders(t, cellsliceop.CHASHIX().Serialize()), stack: []any{deep, int64(idx)}, exit: 0},
			cellParityCase{name: fmt.Sprintf("cdepthix_%d", idx), code: codeFromBuilders(t, cellsliceop.CDEPTHIX().Serialize()), stack: []any{deep, int64(idx)}, exit: 0},
		)
	}

	left := matrixSliceFromBits(t, "101100")
	equal := matrixSliceFromBits(t, "101100")
	prefix := matrixSliceFromBits(t, "101")
	properSuffix := matrixSliceFromBits(t, "100")
	greater := matrixSliceFromBits(t, "101101")
	tests = append(tests,
		cellParityCase{name: "sdeq_equal", code: codeFromBuilders(t, cellsliceop.SDEQ().Serialize()), stack: []any{left, equal}, exit: 0},
		cellParityCase{name: "sdlexcmp_less", code: codeFromBuilders(t, cellsliceop.SDLEXCMP().Serialize()), stack: []any{left, greater}, exit: 0},
		cellParityCase{name: "sdlexcmp_equal", code: codeFromBuilders(t, cellsliceop.SDLEXCMP().Serialize()), stack: []any{left, equal}, exit: 0},
		cellParityCase{name: "sdlexcmp_greater", code: codeFromBuilders(t, cellsliceop.SDLEXCMP().Serialize()), stack: []any{greater, left}, exit: 0},
		cellParityCase{name: "sdpfx_hit", code: codeFromBuilders(t, cellsliceop.SDPFX().Serialize()), stack: []any{prefix, left}, exit: 0},
		cellParityCase{name: "sdpfxrev_hit", code: codeFromBuilders(t, cellsliceop.SDPFXREV().Serialize()), stack: []any{left, prefix}, exit: 0},
		cellParityCase{name: "sdppfx_hit", code: codeFromBuilders(t, cellsliceop.SDPPFX().Serialize()), stack: []any{prefix, left}, exit: 0},
		cellParityCase{name: "sdppfxrev_hit", code: codeFromBuilders(t, cellsliceop.SDPPFXREV().Serialize()), stack: []any{left, prefix}, exit: 0},
		cellParityCase{name: "sdppfx_equal_false", code: codeFromBuilders(t, cellsliceop.SDPPFX().Serialize()), stack: []any{left, equal}, exit: 0},
		cellParityCase{name: "sdsfx_hit", code: codeFromBuilders(t, cellsliceop.SDSFX().Serialize()), stack: []any{properSuffix, left}, exit: 0},
		cellParityCase{name: "sdsfxrev_hit", code: codeFromBuilders(t, cellsliceop.SDSFXREV().Serialize()), stack: []any{left, properSuffix}, exit: 0},
		cellParityCase{name: "sdpsfx_hit", code: codeFromBuilders(t, cellsliceop.SDPSFX().Serialize()), stack: []any{properSuffix, left}, exit: 0},
		cellParityCase{name: "sdpsfxrev_hit", code: codeFromBuilders(t, cellsliceop.SDPSFXREV().Serialize()), stack: []any{left, properSuffix}, exit: 0},
		cellParityCase{name: "sdpsfx_equal_false", code: codeFromBuilders(t, cellsliceop.SDPSFX().Serialize()), stack: []any{left, equal}, exit: 0},
		cellParityCase{name: "sdbeginsx_hit", code: codeFromBuilders(t, cellsliceop.SDBEGINSX().Serialize()), stack: []any{left, prefix}, exit: 0},
		cellParityCase{name: "sdbeginsx_miss", code: codeFromBuilders(t, cellsliceop.SDBEGINSX().Serialize()), stack: []any{left, properSuffix}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "sdbeginsxq_miss", code: codeFromBuilders(t, cellsliceop.SDBEGINSXQ().Serialize()), stack: []any{left, properSuffix}, exit: 0},
	)

	counterPattern := matrixSliceFromBits(t, "0011100")
	tests = append(tests,
		cellParityCase{name: "sempty_empty", code: codeFromBuilders(t, cellsliceop.SEMPTY().Serialize()), stack: []any{matrixSlice(t, 0, 0)}, exit: 0},
		cellParityCase{name: "sempty_ref_only", code: codeFromBuilders(t, cellsliceop.SEMPTY().Serialize()), stack: []any{matrixSlice(t, 0, 1)}, exit: 0},
		cellParityCase{name: "sdempty_ref_only", code: codeFromBuilders(t, cellsliceop.SDEMPTY().Serialize()), stack: []any{matrixSlice(t, 0, 1)}, exit: 0},
		cellParityCase{name: "srempty_bit_only", code: codeFromBuilders(t, cellsliceop.SREMPTY().Serialize()), stack: []any{matrixSlice(t, 1, 0)}, exit: 0},
		cellParityCase{name: "sdfirst_empty_false", code: codeFromBuilders(t, cellsliceop.SDFIRST().Serialize()), stack: []any{matrixSlice(t, 0, 0)}, exit: 0},
		cellParityCase{name: "sdfirst_zero_false", code: codeFromBuilders(t, cellsliceop.SDFIRST().Serialize()), stack: []any{matrixSliceFromBits(t, "0")}, exit: 0},
		cellParityCase{name: "sdfirst_one_true", code: codeFromBuilders(t, cellsliceop.SDFIRST().Serialize()), stack: []any{matrixSliceFromBits(t, "1")}, exit: 0},
		cellParityCase{name: "sdcntlead0_pattern", code: codeFromBuilders(t, cellsliceop.SDCNTLEAD0().Serialize()), stack: []any{counterPattern}, exit: 0},
		cellParityCase{name: "sdcnttrail0_pattern", code: codeFromBuilders(t, cellsliceop.SDCNTTRAIL0().Serialize()), stack: []any{counterPattern}, exit: 0},
		cellParityCase{name: "ldzeroes_success", code: codeFromBuilders(t, cellsliceop.LDZEROES().Serialize()), stack: []any{matrixSliceFromBits(t, "00101")}, exit: 0},
		cellParityCase{name: "ldones_success", code: codeFromBuilders(t, cellsliceop.LDONES().Serialize()), stack: []any{matrixSliceFromBits(t, "1110")}, exit: 0},
		cellParityCase{name: "ldsame_count0", code: codeFromBuilders(t, cellsliceop.LDSAME().Serialize()), stack: []any{matrixSliceFromBits(t, "0111"), int64(1)}, exit: 0},
	)

	le4 := cell.BeginCell().MustStoreSlice([]byte{0xFE, 0xFF, 0xFF, 0xFF, 0xA5}, 40).EndCell().MustBeginParse()
	le8 := cell.BeginCell().MustStoreSlice([]byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0xA5}, 72).EndCell().MustBeginParse()
	short4 := cell.BeginCell().MustStoreSlice([]byte{0xAA, 0xBB, 0xCC, 0xDD}, 31).EndCell().MustBeginParse()
	short8 := cell.BeginCell().MustStoreSlice([]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}, 63).EndCell().MustBeginParse()
	tests = append(tests,
		cellParityCase{name: "ldile4_empty_stack", code: codeFromBuilders(t, cellsliceop.LDILE4().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "ldule8_typecheck", code: codeFromBuilders(t, cellsliceop.LDULE8().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "ldile4_success", code: codeFromBuilders(t, cellsliceop.LDILE4().Serialize()), stack: []any{le4}, exit: 0},
		cellParityCase{name: "ldule4_success", code: codeFromBuilders(t, cellsliceop.LDULE4().Serialize()), stack: []any{le4}, exit: 0},
		cellParityCase{name: "pldile4_success", code: codeFromBuilders(t, cellsliceop.PLDILE4().Serialize()), stack: []any{le4}, exit: 0},
		cellParityCase{name: "pldule4_success", code: codeFromBuilders(t, cellsliceop.PLDULE4().Serialize()), stack: []any{le4}, exit: 0},
		cellParityCase{name: "ldile8_success", code: codeFromBuilders(t, cellsliceop.LDILE8().Serialize()), stack: []any{le8}, exit: 0},
		cellParityCase{name: "ldule8_success", code: codeFromBuilders(t, cellsliceop.LDULE8().Serialize()), stack: []any{le8}, exit: 0},
		cellParityCase{name: "pldile8_success", code: codeFromBuilders(t, cellsliceop.PLDILE8().Serialize()), stack: []any{le8}, exit: 0},
		cellParityCase{name: "pldule8_success", code: codeFromBuilders(t, cellsliceop.PLDULE8().Serialize()), stack: []any{le8}, exit: 0},
		cellParityCase{name: "ldile4q_success", code: codeFromBuilders(t, cellsliceop.LDILE4Q().Serialize()), stack: []any{le4}, exit: 0},
		cellParityCase{name: "ldule4q_success", code: codeFromBuilders(t, cellsliceop.LDULE4Q().Serialize()), stack: []any{le4}, exit: 0},
		cellParityCase{name: "pldile4q_success", code: codeFromBuilders(t, cellsliceop.PLDILE4Q().Serialize()), stack: []any{le4}, exit: 0},
		cellParityCase{name: "pldule4q_success", code: codeFromBuilders(t, cellsliceop.PLDULE4Q().Serialize()), stack: []any{le4}, exit: 0},
		cellParityCase{name: "ldile8q_success", code: codeFromBuilders(t, cellsliceop.LDILE8Q().Serialize()), stack: []any{le8}, exit: 0},
		cellParityCase{name: "ldule8q_success", code: codeFromBuilders(t, cellsliceop.LDULE8Q().Serialize()), stack: []any{le8}, exit: 0},
		cellParityCase{name: "pldile8q_success", code: codeFromBuilders(t, cellsliceop.PLDILE8Q().Serialize()), stack: []any{le8}, exit: 0},
		cellParityCase{name: "pldule8q_success", code: codeFromBuilders(t, cellsliceop.PLDULE8Q().Serialize()), stack: []any{le8}, exit: 0},
		cellParityCase{name: "ldile4_underflow", code: codeFromBuilders(t, cellsliceop.LDILE4().Serialize()), stack: []any{short4}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "ldule4_underflow", code: codeFromBuilders(t, cellsliceop.LDULE4().Serialize()), stack: []any{short4}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "pldile4_underflow", code: codeFromBuilders(t, cellsliceop.PLDILE4().Serialize()), stack: []any{short4}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "pldule4_underflow", code: codeFromBuilders(t, cellsliceop.PLDULE4().Serialize()), stack: []any{short4}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "ldile8_underflow", code: codeFromBuilders(t, cellsliceop.LDILE8().Serialize()), stack: []any{short8}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "ldule8_underflow", code: codeFromBuilders(t, cellsliceop.LDULE8().Serialize()), stack: []any{short8}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "pldile8_underflow", code: codeFromBuilders(t, cellsliceop.PLDILE8().Serialize()), stack: []any{short8}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "pldule8_underflow", code: codeFromBuilders(t, cellsliceop.PLDULE8().Serialize()), stack: []any{short8}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "ldile4q_underflow", code: codeFromBuilders(t, cellsliceop.LDILE4Q().Serialize()), stack: []any{short4}, exit: 0},
		cellParityCase{name: "ldule4q_underflow", code: codeFromBuilders(t, cellsliceop.LDULE4Q().Serialize()), stack: []any{short4}, exit: 0},
		cellParityCase{name: "pldile4q_underflow", code: codeFromBuilders(t, cellsliceop.PLDILE4Q().Serialize()), stack: []any{short4}, exit: 0},
		cellParityCase{name: "pldule4q_underflow", code: codeFromBuilders(t, cellsliceop.PLDULE4Q().Serialize()), stack: []any{short4}, exit: 0},
		cellParityCase{name: "ldile8q_underflow", code: codeFromBuilders(t, cellsliceop.LDILE8Q().Serialize()), stack: []any{short8}, exit: 0},
		cellParityCase{name: "ldule8q_underflow", code: codeFromBuilders(t, cellsliceop.LDULE8Q().Serialize()), stack: []any{short8}, exit: 0},
		cellParityCase{name: "pldile8q_underflow", code: codeFromBuilders(t, cellsliceop.PLDILE8Q().Serialize()), stack: []any{short8}, exit: 0},
		cellParityCase{name: "pldule8q_underflow", code: codeFromBuilders(t, cellsliceop.PLDULE8Q().Serialize()), stack: []any{short8}, exit: 0},
	)

	tests = append(tests,
		cellParityCase{name: "stile4_success", code: codeFromBuilders(t, cellsliceop.STILE4().Serialize()), stack: []any{int64(-2), cell.BeginCell()}, exit: 0},
		cellParityCase{name: "stule4_success", code: codeFromBuilders(t, cellsliceop.STULE4().Serialize()), stack: []any{int64(0x11223344), cell.BeginCell()}, exit: 0},
		cellParityCase{name: "stile8_success", code: codeFromBuilders(t, cellsliceop.STILE8().Serialize()), stack: []any{big.NewInt(-2), cell.BeginCell()}, exit: 0},
		cellParityCase{name: "stule8_success", code: codeFromBuilders(t, cellsliceop.STULE8().Serialize()), stack: []any{new(big.Int).SetUint64(^uint64(0)), cell.BeginCell()}, exit: 0},
		cellParityCase{name: "stile4_rangecheck", code: codeFromBuilders(t, cellsliceop.STILE4().Serialize()), stack: []any{int64(1 << 31), cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "stule4_negative_rangecheck", code: codeFromBuilders(t, cellsliceop.STULE4().Serialize()), stack: []any{int64(-1), cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "stile8_rangecheck", code: codeFromBuilders(t, cellsliceop.STILE8().Serialize()), stack: []any{new(big.Int).Lsh(big.NewInt(1), 63), cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "stule8_negative_rangecheck", code: codeFromBuilders(t, cellsliceop.STULE8().Serialize()), stack: []any{big.NewInt(-1), cell.BeginCell()}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "stile4_overflow", code: codeFromBuilders(t, cellsliceop.STILE4().Serialize()), stack: []any{int64(1), matrixBuilder(t, 1000, 0)}, exit: vmerr.CodeCellOverflow},
		cellParityCase{name: "stule8_overflow", code: codeFromBuilders(t, cellsliceop.STULE8().Serialize()), stack: []any{int64(1), matrixBuilder(t, 960, 0)}, exit: vmerr.CodeCellOverflow},
		cellParityCase{name: "stule8_overflow_precedes_range", code: codeFromBuilders(t, cellsliceop.STULE8().Serialize()), stack: []any{int64(-1), matrixBuilder(t, 960, 0)}, exit: vmerr.CodeCellOverflow},
	)

	builderWithDepth := cell.BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell())
	fullBits := matrixBuilder(t, 1023, 0)
	fullRefs := matrixBuilder(t, 0, 4)
	fullBuilder := matrixBuilder(t, 1023, 4)
	constRef := matrixCell(t, 8, 0)
	constSlice := matrixSliceFromBits(t, "101")
	tests = append(tests,
		cellParityCase{name: "bdepth_nested", code: codeFromBuilders(t, cellsliceop.BDEPTH().Serialize()), stack: []any{builderWithDepth}, exit: 0},
		cellParityCase{name: "bbits_nested", code: codeFromBuilders(t, cellsliceop.BBITS().Serialize()), stack: []any{builderWithDepth}, exit: 0},
		cellParityCase{name: "brefs_nested", code: codeFromBuilders(t, cellsliceop.BREFS().Serialize()), stack: []any{builderWithDepth}, exit: 0},
		cellParityCase{name: "bbitrefs_nested", code: codeFromBuilders(t, cellsliceop.BBITREFS().Serialize()), stack: []any{builderWithDepth}, exit: 0},
		cellParityCase{name: "bdepth_empty_stack", code: codeFromBuilders(t, cellsliceop.BDEPTH().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "bbitrefs_empty_stack", code: codeFromBuilders(t, cellsliceop.BBITREFS().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "bbitrefs_typecheck", code: codeFromBuilders(t, cellsliceop.BBITREFS().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "split_bits_refs_success", code: codeFromBuilders(t, cellsliceop.SPLIT().Serialize()), stack: []any{matrixSlice(t, 8, 2), int64(4), int64(1)}, exit: 0},
		cellParityCase{name: "splitq_bits_refs_success", code: codeFromBuilders(t, cellsliceop.SPLITQ().Serialize()), stack: []any{matrixSlice(t, 8, 2), int64(4), int64(1)}, exit: 0},
		cellParityCase{name: "bchkbits_empty_stack", code: codeFromBuilders(t, cellsliceop.BCHKBITS().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "bchkbits_bits_typecheck", code: codeFromBuilders(t, cellsliceop.BCHKBITS().Serialize()), stack: []any{cell.BeginCell(), matrixCell(t, 0, 0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "bchkbits_builder_typecheck", code: codeFromBuilders(t, cellsliceop.BCHKBITS().Serialize()), stack: []any{int64(0), int64(1)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "bchkbits_range_1024", code: codeFromBuilders(t, cellsliceop.BCHKBITS().Serialize()), stack: []any{cell.BeginCell(), int64(1024)}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "bchkbits_success", code: codeFromBuilders(t, cellsliceop.BCHKBITS().Serialize()), stack: []any{matrixBuilder(t, 1015, 0), int64(8)}, exit: 0},
		cellParityCase{name: "bchkbits_overflow", code: codeFromBuilders(t, cellsliceop.BCHKBITS().Serialize()), stack: []any{fullBits, int64(1)}, exit: vmerr.CodeCellOverflow},
		cellParityCase{name: "bchkrefs_empty_stack", code: codeFromBuilders(t, cellsliceop.BCHKREFS().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "bchkrefs_refs_typecheck", code: codeFromBuilders(t, cellsliceop.BCHKREFS().Serialize()), stack: []any{cell.BeginCell(), matrixCell(t, 0, 0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "bchkrefs_range_8", code: codeFromBuilders(t, cellsliceop.BCHKREFS().Serialize()), stack: []any{cell.BeginCell(), int64(8)}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "bchkrefs_success", code: codeFromBuilders(t, cellsliceop.BCHKREFS().Serialize()), stack: []any{matrixBuilder(t, 0, 3), int64(1)}, exit: 0},
		cellParityCase{name: "bchkrefs_overflow", code: codeFromBuilders(t, cellsliceop.BCHKREFS().Serialize()), stack: []any{fullRefs, int64(1)}, exit: vmerr.CodeCellOverflow},
		cellParityCase{name: "bchkbitrefs_empty_stack", code: codeFromBuilders(t, cellsliceop.BCHKBITREFS().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "bchkbitrefs_refs_range_8", code: codeFromBuilders(t, cellsliceop.BCHKBITREFS().Serialize()), stack: []any{cell.BeginCell(), int64(1024), int64(8)}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "bchkbitrefs_bits_range_1024", code: codeFromBuilders(t, cellsliceop.BCHKBITREFS().Serialize()), stack: []any{cell.BeginCell(), int64(1024), int64(0)}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "bchkbitrefs_builder_typecheck", code: codeFromBuilders(t, cellsliceop.BCHKBITREFS().Serialize()), stack: []any{int64(0), int64(1), int64(1)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "bchkbitrefs_success", code: codeFromBuilders(t, cellsliceop.BCHKBITREFS().Serialize()), stack: []any{matrixBuilder(t, 1000, 3), int64(23), int64(1)}, exit: 0},
		cellParityCase{name: "bchkbitrefs_overflow_bits", code: codeFromBuilders(t, cellsliceop.BCHKBITREFS().Serialize()), stack: []any{matrixBuilder(t, 1000, 3), int64(24), int64(1)}, exit: vmerr.CodeCellOverflow},
		cellParityCase{name: "bchkbitsq_success", code: codeFromBuilders(t, cellsliceop.BCHKBITSQ().Serialize()), stack: []any{matrixBuilder(t, 1015, 0), int64(8)}, exit: 0},
		cellParityCase{name: "bchkbitsq_fail", code: codeFromBuilders(t, cellsliceop.BCHKBITSQ().Serialize()), stack: []any{fullBits, int64(1)}, exit: 0},
		cellParityCase{name: "bchkrefsq_success", code: codeFromBuilders(t, cellsliceop.BCHKREFSQ().Serialize()), stack: []any{matrixBuilder(t, 0, 3), int64(1)}, exit: 0},
		cellParityCase{name: "bchkrefsq_fail", code: codeFromBuilders(t, cellsliceop.BCHKREFSQ().Serialize()), stack: []any{fullRefs, int64(1)}, exit: 0},
		cellParityCase{name: "bchkbitrefsq_success", code: codeFromBuilders(t, cellsliceop.BCHKBITREFSQ().Serialize()), stack: []any{matrixBuilder(t, 1000, 3), int64(23), int64(1)}, exit: 0},
		cellParityCase{name: "bchkbitrefsq_fail_refs", code: codeFromBuilders(t, cellsliceop.BCHKBITREFSQ().Serialize()), stack: []any{matrixBuilder(t, 1000, 4), int64(23), int64(1)}, exit: 0},
		cellParityCase{name: "bchkbitsimm_empty_stack", code: codeFromBuilders(t, cellsliceop.BCHKBITSIMM(24, false).Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "bchkbitsimm_typecheck", code: codeFromBuilders(t, cellsliceop.BCHKBITSIMM(24, false).Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "bchkbitsimm_success", code: codeFromBuilders(t, cellsliceop.BCHKBITSIMM(24, false).Serialize()), stack: []any{matrixBuilder(t, 999, 0)}, exit: 0},
		cellParityCase{name: "bchkbitsimm_overflow", code: codeFromBuilders(t, cellsliceop.BCHKBITSIMM(24, false).Serialize()), stack: []any{matrixBuilder(t, 1000, 0)}, exit: vmerr.CodeCellOverflow},
		cellParityCase{name: "bchkbitsimmq_empty_stack", code: codeFromBuilders(t, cellsliceop.BCHKBITSIMM(24, true).Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "bchkbitsimmq_typecheck", code: codeFromBuilders(t, cellsliceop.BCHKBITSIMM(24, true).Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "bchkbitsimmq_success", code: codeFromBuilders(t, cellsliceop.BCHKBITSIMM(24, true).Serialize()), stack: []any{matrixBuilder(t, 999, 0)}, exit: 0},
		cellParityCase{name: "bchkbitsimmq_fail", code: codeFromBuilders(t, cellsliceop.BCHKBITSIMM(24, true).Serialize()), stack: []any{matrixBuilder(t, 1000, 0)}, exit: 0},
	)

	tests = append(tests,
		cellParityCase{name: "stzeroes_zero_bits", code: codeFromBuilders(t, cellsliceop.STZEROES().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: 0},
		cellParityCase{name: "stzeroes_overflow", code: codeFromBuilders(t, cellsliceop.STZEROES().Serialize()), stack: []any{fullBits, int64(1)}, exit: vmerr.CodeCellOverflow},
		cellParityCase{name: "stones_one_bit", code: codeFromBuilders(t, cellsliceop.STONES().Serialize()), stack: []any{matrixBuilder(t, 1022, 0), int64(1)}, exit: 0},
		cellParityCase{name: "stzeroes_bits_typecheck", code: codeFromBuilders(t, cellsliceop.STZEROES().Serialize()), stack: []any{cell.BeginCell(), matrixCell(t, 0, 0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "stzeroes_builder_typecheck", code: codeFromBuilders(t, cellsliceop.STZEROES().Serialize()), stack: []any{matrixCell(t, 0, 0), int64(1)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "stones_rangecheck", code: codeFromBuilders(t, cellsliceop.STONES().Serialize()), stack: []any{cell.BeginCell(), int64(1024)}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "stsame_zeroes", code: codeFromBuilders(t, cellsliceop.STSAME().Serialize()), stack: []any{cell.BeginCell(), int64(4), int64(0)}, exit: 0},
		cellParityCase{name: "stsame_ones", code: codeFromBuilders(t, cellsliceop.STSAME().Serialize()), stack: []any{cell.BeginCell(), int64(4), int64(1)}, exit: 0},
		cellParityCase{name: "stsame_bit_rangecheck", code: codeFromBuilders(t, cellsliceop.STSAME().Serialize()), stack: []any{cell.BeginCell(), int64(1), int64(2)}, exit: vmerr.CodeRangeCheck},
		cellParityCase{name: "btos_empty_stack", code: codeFromBuilders(t, cellsliceop.BTOS().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "btos_empty", code: codeFromBuilders(t, cellsliceop.BTOS().Serialize()), stack: []any{cell.BeginCell()}, exit: 0},
		cellParityCase{name: "btos_bits_refs", code: codeFromBuilders(t, cellsliceop.BTOS().Serialize()), stack: []any{builderWithDepth}, exit: 0},
		cellParityCase{name: "btos_typecheck", code: codeFromBuilders(t, cellsliceop.BTOS().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "strefconst_success", code: codeFromBuilders(t, cellsliceop.STREFCONST(constRef).Serialize()), stack: []any{cell.BeginCell()}, exit: 0},
		cellParityCase{name: "stref2const_success", code: codeFromBuilders(t, cellsliceop.STREF2CONST(constRef, matrixCell(t, 0, 1)).Serialize()), stack: []any{cell.BeginCell()}, exit: 0},
		cellParityCase{name: "strefconst_overflow", code: codeFromBuilders(t, cellsliceop.STREFCONST(constRef).Serialize()), stack: []any{fullRefs}, exit: vmerr.CodeCellOverflow},
		cellParityCase{name: "stsliceconst_success", code: codeFromBuilders(t, cellsliceop.STSLICECONST(constSlice).Serialize()), stack: []any{cell.BeginCell()}, exit: 0},
		cellParityCase{name: "stsliceconst_overflow", code: codeFromBuilders(t, cellsliceop.STSLICECONST(constSlice).Serialize()), stack: []any{fullBits}, exit: vmerr.CodeCellOverflow},
		cellParityCase{name: "endxc_ordinary_empty", code: codeFromBuilders(t, cellsliceop.ENDXC().Serialize()), stack: []any{cell.BeginCell(), int64(0)}, exit: 0},
		cellParityCase{name: "endxc_ordinary_refs", code: codeFromBuilders(t, cellsliceop.ENDXC().Serialize()), stack: []any{builderWithDepth, int64(0)}, exit: 0},
		cellParityCase{name: "endxc_library_special", code: codeFromBuilders(t, cellsliceop.ENDXC().Serialize()), stack: []any{mustLibraryBuilder(t), int64(-1)}, exit: 0},
		cellParityCase{name: "endxc_invalid_special", code: codeFromBuilders(t, cellsliceop.ENDXC().Serialize()), stack: []any{cell.BeginCell(), int64(-1)}, exit: vmerr.CodeCellOverflow},
		cellParityCase{name: "brembits_empty", code: codeFromBuilders(t, cellsliceop.BREMBITS().Serialize()), stack: []any{cell.BeginCell()}, exit: 0},
		cellParityCase{name: "brembits_full", code: codeFromBuilders(t, cellsliceop.BREMBITS().Serialize()), stack: []any{fullBuilder}, exit: 0},
		cellParityCase{name: "bremrefs_empty", code: codeFromBuilders(t, cellsliceop.BREMREFS().Serialize()), stack: []any{cell.BeginCell()}, exit: 0},
		cellParityCase{name: "bremrefs_full", code: codeFromBuilders(t, cellsliceop.BREMREFS().Serialize()), stack: []any{fullBuilder}, exit: 0},
		cellParityCase{name: "brembitrefs_empty", code: codeFromBuilders(t, cellsliceop.BREMBITREFS().Serialize()), stack: []any{cell.BeginCell()}, exit: 0},
		cellParityCase{name: "brembitrefs_full", code: codeFromBuilders(t, cellsliceop.BREMBITREFS().Serialize()), stack: []any{fullBuilder}, exit: 0},
		cellParityCase{name: "brembitrefs_empty_stack", code: codeFromBuilders(t, cellsliceop.BREMBITREFS().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "brembitrefs_typecheck", code: codeFromBuilders(t, cellsliceop.BREMBITREFS().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "sbitrefs_empty_stack", code: codeFromBuilders(t, cellsliceop.SBITREFS().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "sbitrefs_typecheck", code: codeFromBuilders(t, cellsliceop.SBITREFS().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
	)

	ordinary := matrixCell(t, 8, 1)
	tests = append(tests,
		cellParityCase{name: "xctos_empty_stack", code: codeFromBuilders(t, cellsliceop.XCTOS().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "xctos_typecheck", code: codeFromBuilders(t, cellsliceop.XCTOS().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "xctos_ordinary", code: codeFromBuilders(t, cellsliceop.XCTOS().Serialize()), stack: []any{ordinary}, exit: 0},
		cellParityCase{name: "xctos_library_special", code: codeFromBuilders(t, cellsliceop.XCTOS().Serialize()), stack: []any{mustLibraryCell(t)}, exit: 0},
		cellParityCase{name: "xload_empty_stack", code: codeFromBuilders(t, cellsliceop.XLOAD().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "xload_typecheck", code: codeFromBuilders(t, cellsliceop.XLOAD().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "xload_ordinary", code: codeFromBuilders(t, cellsliceop.XLOAD().Serialize()), stack: []any{ordinary}, exit: 0},
		cellParityCase{name: "xloadq_empty_stack", code: codeFromBuilders(t, cellsliceop.XLOADQ().Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "xloadq_typecheck", code: codeFromBuilders(t, cellsliceop.XLOADQ().Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "xloadq_ordinary", code: codeFromBuilders(t, cellsliceop.XLOADQ().Serialize()), stack: []any{ordinary}, exit: 0},
	)

	constPrefix := matrixSliceFromBits(t, "101")
	constHaystack := matrixSliceFromBits(t, "101100")
	constExact := matrixSliceFromBits(t, "101")
	constMiss := matrixSliceFromBits(t, "100101")
	tests = append(tests,
		cellParityCase{name: "sdbeginsconst_empty_stack", code: codeFromBuilders(t, cellsliceop.SDBEGINSCONST(constPrefix, false).Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "sdbeginsconst_typecheck", code: codeFromBuilders(t, cellsliceop.SDBEGINSCONST(constPrefix, false).Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "sdbeginsconst_prefix", code: codeFromBuilders(t, cellsliceop.SDBEGINSCONST(constPrefix, false).Serialize()), stack: []any{constHaystack}, exit: 0},
		cellParityCase{name: "sdbeginsconst_exact", code: codeFromBuilders(t, cellsliceop.SDBEGINSCONST(constPrefix, false).Serialize()), stack: []any{constExact}, exit: 0},
		cellParityCase{name: "sdbeginsconst_miss", code: codeFromBuilders(t, cellsliceop.SDBEGINSCONST(constPrefix, false).Serialize()), stack: []any{constMiss}, exit: vmerr.CodeCellUnderflow},
		cellParityCase{name: "sdbeginsconstq_empty_stack", code: codeFromBuilders(t, cellsliceop.SDBEGINSCONST(constPrefix, true).Serialize()), exit: vmerr.CodeStackUnderflow},
		cellParityCase{name: "sdbeginsconstq_typecheck", code: codeFromBuilders(t, cellsliceop.SDBEGINSCONST(constPrefix, true).Serialize()), stack: []any{int64(0)}, exit: vmerr.CodeTypeCheck},
		cellParityCase{name: "sdbeginsconstq_prefix", code: codeFromBuilders(t, cellsliceop.SDBEGINSCONST(constPrefix, true).Serialize()), stack: []any{constHaystack}, exit: 0},
		cellParityCase{name: "sdbeginsconstq_exact", code: codeFromBuilders(t, cellsliceop.SDBEGINSCONST(constPrefix, true).Serialize()), stack: []any{constExact}, exit: 0},
		cellParityCase{name: "sdbeginsconstq_miss", code: codeFromBuilders(t, cellsliceop.SDBEGINSCONST(constPrefix, true).Serialize()), stack: []any{constMiss}, exit: 0},
	)

	return tests
}

func TestTVMCrossEmulatorCellOpsMatrixAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := cellOpsMatrixCases(t)
	if len(tests) != cellOpsMatrixCaseCount {
		t.Fatalf("cellops matrix case count = %d, want %d", len(tests), cellOpsMatrixCaseCount)
	}
	versions := crossEmulatorVersionAuditVersions(t, "TVM_CELLOPS_MATRIX_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runCellParityCaseWithVersion(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorCellOpsMatrixGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(version%cellOpsMatrixCaseCount))
	}
	for i := 0; i < cellOpsMatrixCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint16(i))
	}
	f.Add(uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := cellOpsMatrixCases(t)
		if len(tests) != cellOpsMatrixCaseCount {
			t.Fatalf("cellops matrix case count = %d, want %d", len(tests), cellOpsMatrixCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runCellParityCaseWithVersion(t, tt, version)
	})
}

const cellOpsMatrixCaseCount = 465

func runCellParityCases(t *testing.T, tests []cellParityCase) {
	t.Helper()

	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
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
		})
	}
}

func runCellParityCaseWithVersion(t *testing.T, tt cellParityCase, globalVersion int) {
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

	goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack, globalVersion)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
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

func matrixCell(t *testing.T, bits uint, refs int) *cell.Cell {
	t.Helper()
	return matrixBuilder(t, bits, refs).EndCell()
}

func matrixSlice(t *testing.T, bits uint, refs int) *cell.Slice {
	t.Helper()
	return matrixCell(t, bits, refs).MustBeginParse()
}

func matrixBuilder(t *testing.T, bits uint, refs int) *cell.Builder {
	t.Helper()
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreSlice(matrixPattern(bits), bits)
	}
	for i := 0; i < refs; i++ {
		b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
	}
	return b
}

func matrixSliceFromBits(t *testing.T, bits string) *cell.Slice {
	t.Helper()
	b := cell.BeginCell()
	for _, bit := range bits {
		if bit == '1' {
			b.MustStoreUInt(1, 1)
		} else {
			b.MustStoreUInt(0, 1)
		}
	}
	return b.EndCell().MustBeginParse()
}

func matrixPattern(bits uint) []byte {
	data := make([]byte, (bits+7)/8)
	for i := range data {
		data[i] = byte(0xA5 ^ (i * 37))
	}
	return data
}

func matrixDeepCell(t *testing.T) *cell.Cell {
	t.Helper()
	child := cell.BeginCell().MustStoreUInt(0xA, 4).EndCell()
	return cell.BeginCell().MustStoreUInt(0xB, 4).MustStoreRef(child).EndCell()
}

func cellRefLoadExit(refs int) int32 {
	if refs == 0 {
		return vmerr.CodeCellUnderflow
	}
	return 0
}

func storeIntVarExtCode(mode uint8) *cell.Cell {
	return cell.BeginCell().MustStoreUInt(uint64(0xCF00)|uint64(mode), 16).EndCell()
}

func storeIntFixedExtCode(mode uint8, bits uint) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0xCF08>>3, 13).
		MustStoreUInt((uint64(mode)<<8)|uint64(bits-1), 11).
		EndCell()
}
