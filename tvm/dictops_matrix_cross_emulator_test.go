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

type dictMatrixCrossCase struct {
	name  string
	op    uint16
	code  func(*testing.T) *cell.Cell
	stack func(*testing.T) []any
	exit  int32
}

func TestTVMCrossEmulatorDictOpsMatrix(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := append(dictMatrixLoadCases(),
		dictMatrixValueCases()...,
	)
	tests = append(tests, dictMatrixBuilderCases()...)
	tests = append(tests, dictMatrixDeleteAndOptRefCases()...)
	tests = append(tests, dictMatrixMinMaxCases()...)
	tests = append(tests, dictMatrixNearCases()...)
	tests = append(tests, dictMatrixPrefixCases()...)
	tests = append(tests, dictMatrixSubdictCases()...)
	tests = append(tests, dictMatrixJumpExecCases()...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := tt.code
			if code == nil {
				code = dictMatrixOpcode(tt.op)
			}

			runDictMatrixCrossCase(t, prependRawMethodDrop(code(t)), tt.stack(t), tt.exit)
		})
	}
}

func dictMatrixLoadCases() []dictMatrixCrossCase {
	return []dictMatrixCrossCase{
		{
			name: "load/lddictq_invalid_quiet",
			op:   0xF406,
			stack: func(t *testing.T) []any {
				return []any{cell.BeginCell().EndCell().MustBeginParse()}
			},
			exit: 0,
		},
		{
			name: "load/stdict_non_empty",
			op:   0xF400,
			stack: func(t *testing.T) []any {
				return []any{
					mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8),
					cell.BeginCell().MustStoreUInt(0xA, 4),
				}
			},
			exit: 0,
		},
		{
			name: "load/skipdict_non_empty",
			op:   0xF401,
			stack: func(t *testing.T) []any {
				root := mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
				return []any{cell.BeginCell().MustStoreMaybeRef(root).MustStoreUInt(0xA, 4).ToSlice()}
			},
			exit: 0,
		},
	}
}

func dictMatrixValueCases() []dictMatrixCrossCase {
	return []dictMatrixCrossCase{
		{
			name: "value/get_short_stack_bad_n_order",
			op:   0xF40A,
			stack: func(t *testing.T) []any {
				return []any{int64(1024)}
			},
			exit: vmerr.CodeStackUnderflow,
		},
		{
			name: "value/get_slice_hit",
			op:   0xF40A,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x20, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/get_slice_miss_nil_root",
			op:   0xF40A,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x20, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/get_slice_miss_existing_root",
			op:   0xF40A,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x33, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/get_ref_hit",
			op:   0xF40B,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/get_ref_miss_existing_root",
			op:   0xF40B,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x20, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/iget_negative_hit",
			op:   0xF40C,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/iget_miss_nil_root",
			op:   0xF40C,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/igetref_negative_hit",
			op:   0xF40D,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-2), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/uget_hit",
			op:   0xF40E,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x80), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/uget_out_of_range_miss",
			op:   0xF40E,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(256), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ugetref_hit",
			op:   0xF40F,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xFE), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ugetref_miss_nil_root",
			op:   0xF40F,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xFE), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/set_slice_insert_nil_root",
			op:   0xF412,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xAA, 8), mustSliceKey(t, 0x33, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/set_slice_update_existing",
			op:   0xF412,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xAC, 8), mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/set_ref_update_existing",
			op:   0xF413,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xCAFE, 16), mustSliceKey(t, 0x10, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/set_ref_insert_nil_root",
			op:   0xF413,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xC0DE, 16), mustSliceKey(t, 0x44, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/iset_update_existing",
			op:   0xF414,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xAD, 8), big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/isetref_insert_nil_root",
			op:   0xF415,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xD00D, 16), big.NewInt(-1), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/uset_insert_nil_root",
			op:   0xF416,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xAE, 8), big.NewInt(0x44), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/uset_negative_key_range",
			op:   0xF416,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xAA, 8), big.NewInt(-1), nil, int64(8)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "value/usetref_update_existing",
			op:   0xF417,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xD0D0, 16), big.NewInt(0xFE), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/setget_slice_update_existing",
			op:   0xF41A,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xCC, 8), mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/setget_slice_insert_existing_root",
			op:   0xF41A,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xCD, 8), mustSliceKey(t, 0x44, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/setget_ref_insert_nil_root",
			op:   0xF41B,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xFACE, 16), mustSliceKey(t, 0x77, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/setget_ref_bad_old_value_after_set_gas",
			op:   0xF41B,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xF00D, 16), mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: vmerr.CodeDict,
		},
		{
			name: "value/isetget_hit",
			op:   0xF41C,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xCE, 8), big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/isetgetref_hit",
			op:   0xF41D,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xB0B0, 16), big.NewInt(-2), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/usetget_insert_nil_root",
			op:   0xF41E,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xCF, 8), big.NewInt(0x55), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/usetgetref_update_existing",
			op:   0xF41F,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xB1B1, 16), big.NewInt(0xFE), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/replace_slice_hit",
			op:   0xF422,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xD1, 8), mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/replace_slice_miss",
			op:   0xF422,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xD2, 8), mustSliceKey(t, 0x77, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/replace_ref_hit",
			op:   0xF423,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xABAB, 16), mustSliceKey(t, 0x10, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/replace_ref_miss_nil_root",
			op:   0xF423,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xACAC, 16), mustSliceKey(t, 0x33, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ireplace_hit",
			op:   0xF424,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xD4, 8), big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ireplaceref_miss",
			op:   0xF425,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xADAD, 16), big.NewInt(-1), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ureplace_hit",
			op:   0xF426,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xD5, 8), big.NewInt(0x80), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ureplaceref_hit",
			op:   0xF427,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xAEAE, 16), big.NewInt(0xFE), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/replaceget_slice_miss_nil_root",
			op:   0xF42A,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xD6, 8), mustSliceKey(t, 0x55, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/replaceget_slice_hit",
			op:   0xF42A,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xD7, 8), mustSliceKey(t, 0x20, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/replaceget_ref_hit",
			op:   0xF42B,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xABCD, 16), mustSliceKey(t, 0x10, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ireplaceget_hit",
			op:   0xF42C,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xD8, 8), big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ireplacegetref_miss",
			op:   0xF42D,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xA0A0, 16), big.NewInt(-1), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ureplaceget_miss",
			op:   0xF42E,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xD3, 8), big.NewInt(0x77), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ureplaceget_hit",
			op:   0xF42E,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xD9, 8), big.NewInt(0x80), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/ureplacegetref_hit",
			op:   0xF42F,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xA1A1, 16), big.NewInt(0xFE), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/add_slice_missing_nil_root",
			op:   0xF432,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xE1, 8), mustSliceKey(t, 0x77, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/add_slice_existing",
			op:   0xF432,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xE2, 8), mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/add_ref_missing_nil_root",
			op:   0xF433,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xE0E0, 16), mustSliceKey(t, 0x77, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/add_ref_existing",
			op:   0xF433,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xE1E1, 16), mustSliceKey(t, 0x10, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/iadd_missing",
			op:   0xF434,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xE3, 8), big.NewInt(-3), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/iaddref_existing",
			op:   0xF435,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xE2E2, 16), big.NewInt(-2), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/uadd_missing",
			op:   0xF436,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xE4, 8), big.NewInt(0x77), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/uaddref_existing",
			op:   0xF437,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xE3E3, 16), big.NewInt(0xFE), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/addget_slice_missing_existing_root",
			op:   0xF43A,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xE5, 8), mustSliceKey(t, 0x77, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/addget_slice_existing",
			op:   0xF43A,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xE6, 8), mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/addget_ref_missing_nil_root",
			op:   0xF43B,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0x1234, 16), mustSliceKey(t, 0x77, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/addget_ref_existing",
			op:   0xF43B,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0x5678, 16), mustSliceKey(t, 0x10, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/iaddget_missing",
			op:   0xF43C,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xE7, 8), big.NewInt(-3), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/iaddgetref_existing",
			op:   0xF43D,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xE4E4, 16), big.NewInt(-2), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/uaddget_existing",
			op:   0xF43E,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xE8, 8), big.NewInt(0x80), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/uaddgetref_missing_nil_root",
			op:   0xF43F,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0xE5E5, 16), big.NewInt(0x44), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "value/get_short_slice_key_underflow",
			op:   0xF40A,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x1, 4), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
	}
}

func dictMatrixBuilderCases() []dictMatrixCrossCase {
	return []dictMatrixCrossCase{
		{
			name: "builder/setb_slice_insert_nil_root",
			op:   0xF441,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xA1, 8), mustSliceKey(t, 0x55, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/setb_slice_update_existing",
			op:   0xF441,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xA7, 8), mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/isetb_insert_nil_root",
			op:   0xF442,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xA8, 8), big.NewInt(-1), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/usetb_update_existing",
			op:   0xF443,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xA9, 8), big.NewInt(0x20), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/setgetb_slice_insert_existing_root",
			op:   0xF445,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xAA, 8), mustSliceKey(t, 0x77, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/isetgetb_update_existing",
			op:   0xF446,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xA2, 8), big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/usetgetb_update_existing",
			op:   0xF447,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xAB, 8), big.NewInt(0x80), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/replaceb_slice_hit",
			op:   0xF449,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xAC, 8), mustSliceKey(t, 0x20, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/ireplaceb_hit",
			op:   0xF44A,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xAD, 8), big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/ureplaceb_miss",
			op:   0xF44B,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xA3, 8), big.NewInt(0x77), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/replacegetb_slice_hit",
			op:   0xF44D,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xA4, 8), mustSliceKey(t, 0x20, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/ireplacegetb_miss",
			op:   0xF44E,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xAE, 8), big.NewInt(-3), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/ureplacegetb_hit",
			op:   0xF44F,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xAF, 8), big.NewInt(0x80), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/addb_slice_missing_existing_root",
			op:   0xF451,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xB0, 8), mustSliceKey(t, 0x77, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/iaddb_existing",
			op:   0xF452,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xB1, 8), big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/uaddb_missing_nil_root",
			op:   0xF453,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xA5, 8), big.NewInt(0x77), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/addgetb_slice_existing",
			op:   0xF455,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xA6, 8), mustSliceKey(t, 0x20, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "builder/uaddgetb_missing_existing_root",
			op:   0xF457,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixBuilder(0xB2, 8), big.NewInt(0x77), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
	}
}

func dictMatrixDeleteAndOptRefCases() []dictMatrixCrossCase {
	return []dictMatrixCrossCase{
		{
			name: "delete/del_slice_hit",
			op:   0xF459,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/del_slice_miss_nil_root",
			op:   0xF459,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/idel_hit",
			op:   0xF45A,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/idel_miss",
			op:   0xF45A,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-2), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/udel_miss",
			op:   0xF45B,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x77), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/udel_hit",
			op:   0xF45B,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x80), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/udel_out_of_range_nil_root",
			op:   0xF45B,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(999), nil, int64(8)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "delete/delget_slice_hit",
			op:   0xF462,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x20, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/delget_slice_miss",
			op:   0xF462,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x77, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/delget_ref_hit",
			op:   0xF463,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/delget_ref_miss",
			op:   0xF463,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x20, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/delget_ref_nil_root",
			op:   0xF463,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/idelget_hit",
			op:   0xF464,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/idelgetref_hit",
			op:   0xF465,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-2), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/udelget_miss",
			op:   0xF466,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x77), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/udelget_hit",
			op:   0xF466,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x80), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "delete/udelget_out_of_range_nil_root",
			op:   0xF466,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(999), nil, int64(8)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "delete/udelgetref_hit",
			op:   0xF467,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xFE), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/getoptref_hit",
			op:   0xF469,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/getoptref_miss",
			op:   0xF469,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x77, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/getoptref_nil_root",
			op:   0xF469,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/igetoptref_hit",
			op:   0xF46A,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-2), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/igetoptref_miss_nil_root",
			op:   0xF46A,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/ugetoptref_out_of_range",
			op:   0xF46B,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(999), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/ugetoptref_hit",
			op:   0xF46B,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xFE), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/setgetoptref_insert_nil_root",
			op:   0xF46D,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0x0101, 16), mustSliceKey(t, 0x77, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/setgetoptref_update_existing",
			op:   0xF46D,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0x0202, 16), mustSliceKey(t, 0x10, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/setgetoptref_bad_old_value_after_set_gas",
			op:   0xF46D,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0x0707, 16), mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: vmerr.CodeDict,
		},
		{
			name: "optref/setgetoptref_delete_existing",
			op:   0xF46D,
			stack: func(t *testing.T) []any {
				return []any{nil, mustSliceKey(t, 0x10, 8), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/setgetoptref_delete_miss_nil_root",
			op:   0xF46D,
			stack: func(t *testing.T) []any {
				return []any{nil, mustSliceKey(t, 0x44, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/isetgetoptref_insert_nil_root",
			op:   0xF46E,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0x0404, 16), big.NewInt(-1), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/isetgetoptref_update_existing",
			op:   0xF46E,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0x0505, 16), big.NewInt(-2), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/usetgetoptref_negative_key_range",
			op:   0xF46F,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0x0303, 16), big.NewInt(-1), nil, int64(8)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "optref/usetgetoptref_insert_existing_root",
			op:   0xF46F,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefValue(0x0606, 16), big.NewInt(0x44), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "optref/usetgetoptref_delete_existing",
			op:   0xF46F,
			stack: func(t *testing.T) []any {
				return []any{nil, big.NewInt(0xFE), dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
	}
}

func dictMatrixMinMaxCases() []dictMatrixCrossCase {
	return []dictMatrixCrossCase{
		{
			name: "minmax/min_slice_non_empty",
			op:   0xF482,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/min_ref_non_empty",
			op:   0xF483,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/imin_signed_edge",
			op:   0xF484,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/iminref_signed_edge",
			op:   0xF485,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSignedRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/umin_unsigned_edge",
			op:   0xF486,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixUnsignedEdgeRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/uminref_unsigned_edge",
			op:   0xF487,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixUnsignedRefEdgeRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/max_slice_non_empty",
			op:   0xF48A,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/max_ref_non_empty",
			op:   0xF48B,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/imax_signed_edge",
			op:   0xF48C,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/imaxref_signed_edge",
			op:   0xF48D,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSignedRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/umax_unsigned_edge",
			op:   0xF48E,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixUnsignedEdgeRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/umaxref_unsigned_edge",
			op:   0xF48F,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixUnsignedRefEdgeRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/remmin_slice_non_empty",
			op:   0xF492,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/remmin_ref_non_empty",
			op:   0xF493,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/iremmin_signed_single_to_empty",
			op:   0xF494,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSinglePlainRoot(t, 0x80, 0x11), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/iremminref_signed_non_empty",
			op:   0xF495,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSignedRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/uremmin_unsigned_single_to_empty",
			op:   0xF496,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSinglePlainRoot(t, 0x00, 0xAA), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/uremminref_unsigned_non_empty",
			op:   0xF497,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixUnsignedRefEdgeRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/remmax_slice_single_to_empty",
			op:   0xF49A,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSinglePlainRoot(t, 0x40, 0xBB), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/remmax_ref_non_empty",
			op:   0xF49B,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/iremmax_signed_non_empty",
			op:   0xF49C,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/iremmaxref_signed_non_empty",
			op:   0xF49D,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSignedRefRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/uremmax_unsigned_non_empty",
			op:   0xF49E,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixUnsignedEdgeRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/uremmaxref_unsigned_single_to_empty",
			op:   0xF49F,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSingleRefRoot(t, 0xFF, 0xFFFF), int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/min_empty_nil_root",
			op:   0xF482,
			stack: func(t *testing.T) []any {
				return []any{nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/max_empty_nil_root",
			op:   0xF48A,
			stack: func(t *testing.T) []any {
				return []any{nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/imax_empty_nil_root",
			op:   0xF48C,
			stack: func(t *testing.T) []any {
				return []any{nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/umaxref_empty_nil_root",
			op:   0xF48F,
			stack: func(t *testing.T) []any {
				return []any{nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/iremmin_empty_nil_root",
			op:   0xF494,
			stack: func(t *testing.T) []any {
				return []any{nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/remmax_empty_nil_root",
			op:   0xF49A,
			stack: func(t *testing.T) []any {
				return []any{nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/iremmaxref_empty_nil_root",
			op:   0xF49D,
			stack: func(t *testing.T) []any {
				return []any{nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "minmax/umin_key_len_range",
			op:   0xF486,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixPlainRoot(t), int64(257)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "minmax/imax_key_len_range",
			op:   0xF48C,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSignedPlainRoot(t), int64(258)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "minmax/umax_key_len_range",
			op:   0xF48E,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixPlainRoot(t), int64(257)}
			},
			exit: vmerr.CodeRangeCheck,
		},
	}
}

func dictMatrixNearCases() []dictMatrixCrossCase {
	return []dictMatrixCrossCase{
		{
			name: "near/getnext_slice_hit",
			op:   0xF474,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/getnext_slice_above_max_miss",
			op:   0xF474,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0xF0, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/getnexteq_slice_equal",
			op:   0xF475,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x20, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/getprev_slice_hit",
			op:   0xF476,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x80, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/getprev_slice_at_min_miss",
			op:   0xF476,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/getprev_slice_below_min_miss",
			op:   0xF476,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x01, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/getpreveq_slice_equal_min",
			op:   0xF477,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/igetnext_signed_below_range",
			op:   0xF478,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-200), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/igetnext_signed_above_range_miss",
			op:   0xF478,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(200), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/igetnexteq_signed_min_edge",
			op:   0xF479,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-128), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/igetprev_signed_above_range_uses_max",
			op:   0xF47A,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(200), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/igetprev_signed_below_range_miss",
			op:   0xF47A,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-200), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/igetpreveq_signed_equal",
			op:   0xF47B,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictMatrixSignedPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/ugetnext_negative_uses_min",
			op:   0xF47C,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/ugetnexteq_zero_edge",
			op:   0xF47D,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0), dictMatrixUnsignedEdgeRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/ugetprev_above_range_uses_max",
			op:   0xF47E,
			stack: func(t *testing.T) []any {
				return []any{bigIntFromString(t, "1048576"), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/ugetprev_unsigned_zero_miss",
			op:   0xF47E,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0), dictMatrixUnsignedEdgeRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/ugetprev_negative_miss",
			op:   0xF47E,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictMatrixUnsignedEdgeRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/ugetpreveq_unsigned_max_edge",
			op:   0xF47F,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xFF), dictMatrixUnsignedEdgeRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/ugetnext_above_range_miss",
			op:   0xF47C,
			stack: func(t *testing.T) []any {
				return []any{bigIntFromString(t, "1048576"), dictMatrixPlainRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/getnext_nil_root",
			op:   0xF474,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x10, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/getnexteq_nil_root",
			op:   0xF475,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x20, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/getprev_nil_root",
			op:   0xF476,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x80, 8), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/getpreveq_short_key_underflow",
			op:   0xF477,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0x1, 4), nil, int64(8)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "near/igetnext_signed_below_range_nil_root",
			op:   0xF478,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-200), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/igetpreveq_nil_root",
			op:   0xF47B,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/ugetnext_negative_nil_root",
			op:   0xF47C,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/ugetprev_above_range_nil_root",
			op:   0xF47E,
			stack: func(t *testing.T) []any {
				return []any{bigIntFromString(t, "1048576"), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "near/igetnext_key_len_range",
			op:   0xF478,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(1), nil, int64(258)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "near/ugetnext_key_len_range",
			op:   0xF47C,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(1), nil, int64(257)}
			},
			exit: vmerr.CodeRangeCheck,
		},
	}
}

func dictMatrixPrefixCases() []dictMatrixCrossCase {
	return []dictMatrixCrossCase{
		{
			name: "prefix/pfxdictset_insert_nil_root",
			op:   0xF470,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xA, 4), mustSliceKey(t, 0b10, 2), nil, int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictset_update_existing",
			op:   0xF470,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xF, 4), mustSliceKey(t, 0b10, 2), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictreplace_hit",
			op:   0xF471,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xB, 4), mustSliceKey(t, 0b10, 2), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictreplace_miss",
			op:   0xF471,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xC, 4), mustSliceKey(t, 0b00, 2), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictreplace_nil_root_miss",
			op:   0xF471,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0x1, 4), mustSliceKey(t, 0b00, 2), nil, int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictadd_missing",
			op:   0xF472,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xC, 4), mustSliceKey(t, 0b00, 2), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictadd_existing",
			op:   0xF472,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0xD, 4), mustSliceKey(t, 0b10, 2), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictadd_nil_root",
			op:   0xF472,
			stack: func(t *testing.T) []any {
				return []any{dictMatrixSlice(0x2, 4), mustSliceKey(t, 0b01, 2), nil, int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictdel_hit",
			op:   0xF473,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b10, 2), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictdel_miss",
			op:   0xF473,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b00, 2), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictdel_nil_root",
			op:   0xF473,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b10, 2), nil, int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictgetq_hit",
			op:   0xF4A8,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictgetq_miss",
			op:   0xF4A8,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b0111, 4), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictgetq_nil_root_miss",
			op:   0xF4A8,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4), nil, int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictget_hit",
			op:   0xF4A9,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictget_miss_underflow",
			op:   0xF4A9,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b0111, 4), dictMatrixPrefixRoot(t), int64(4)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "prefix/pfxdictgetjmp_hit",
			op:   0xF4AA,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4), dictMatrixPrefixContRoot(t, 6), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictgetjmp_miss",
			op:   0xF4AA,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b0111, 4), dictMatrixPrefixContRoot(t, 6), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictgetjmp_nil_root_miss",
			op:   0xF4AA,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4), nil, int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictgetexec_hit",
			op:   0xF4AB,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4), dictMatrixPrefixContRoot(t, 7), int64(4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictgetexec_miss_underflow",
			op:   0xF4AB,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b0111, 4), dictMatrixPrefixContRoot(t, 7), int64(4)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "prefix/pfxdictswitch_hit",
			code: func(t *testing.T) *cell.Cell {
				return codeFromBuilders(t, dictop.PFXDICTSWITCH(dictMatrixPrefixContRoot(t, 8), 4).Serialize())
			},
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictswitch_miss",
			code: func(t *testing.T) *cell.Cell {
				return codeFromBuilders(t, dictop.PFXDICTSWITCH(dictMatrixPrefixContRoot(t, 8), 4).Serialize())
			},
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b0111, 4)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictswitch_nil_root_flag_with_ref",
			code: func(t *testing.T) *cell.Cell {
				return cell.BeginCell().
					MustStoreSlice([]byte{0xF4, 0xAC}, 13).
					MustStoreBoolBit(false).
					MustStoreRef(cell.BeginCell().EndCell()).
					MustStoreUInt(4, 10).
					EndCell()
			},
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4)}
			},
			exit: vmerr.CodeStackUnderflow,
		},
		{
			name: "prefix/pfxdictswitch_long_input_hit",
			code: func(t *testing.T) *cell.Cell {
				return codeFromBuilders(t, dictop.PFXDICTSWITCH(dictMatrixPrefixContRoot(t, 8), 4).Serialize())
			},
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b101100, 6)}
			},
			exit: 0,
		},
		{
			name: "prefix/pfxdictgetq_short_stack_underflow_before_type",
			op:   0xF4A8,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b1011, 4)}
			},
			exit: vmerr.CodeStackUnderflow,
		},
	}
}

func dictMatrixSubdictCases() []dictMatrixCrossCase {
	return []dictMatrixCrossCase{
		{
			name: "subdict/short_stack_bad_n_order",
			op:   0xF4B1,
			stack: func(t *testing.T) []any {
				return []any{int64(1024)}
			},
			exit: vmerr.CodeStackUnderflow,
		},
		{
			name: "subdict/slice_hit",
			op:   0xF4B1,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0xA, 4), int64(4), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/slice_remove_prefix_hit",
			op:   0xF4B5,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0xA, 4), int64(4), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/unsigned_hit",
			op:   0xF4B3,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xA), int64(4), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/unsigned_remove_prefix_hit",
			op:   0xF4B7,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xA), int64(4), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/signed_hit",
			op:   0xF4B2,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), int64(2), dictMatrixSignedSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/signed_remove_prefix_hit",
			op:   0xF4B6,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), int64(2), dictMatrixSignedSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/slice_miss_empty",
			op:   0xF4B1,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0xE, 4), int64(4), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/unsigned_miss_empty",
			op:   0xF4B3,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xE), int64(4), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/unsigned_remove_prefix_miss_empty",
			op:   0xF4B7,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xE), int64(4), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/signed_miss_empty",
			op:   0xF4B2,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(1), int64(2), dictMatrixSignedSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/signed_remove_prefix_miss_empty",
			op:   0xF4B6,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(1), int64(2), dictMatrixSignedSubdictRoot(t), int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/slice_nil_root",
			op:   0xF4B1,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0xA, 4), int64(4), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/slice_remove_prefix_nil_root",
			op:   0xF4B5,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0xA, 4), int64(4), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/unsigned_nil_root",
			op:   0xF4B3,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xA), int64(4), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/unsigned_remove_prefix_nil_root",
			op:   0xF4B7,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0xA), int64(4), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/signed_nil_root",
			op:   0xF4B2,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), int64(2), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/signed_remove_prefix_nil_root",
			op:   0xF4B6,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), int64(2), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "subdict/slice_prefix_bits_range",
			op:   0xF4B1,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0, 8), int64(9), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "subdict/slice_prefix_underflow",
			op:   0xF4B1,
			stack: func(t *testing.T) []any {
				return []any{mustSliceKey(t, 0b10, 2), int64(4), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "subdict/unsigned_prefix_bits_range",
			op:   0xF4B3,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0), int64(9), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: vmerr.CodeRangeCheck,
		},
		{
			name: "subdict/unsigned_prefix_value_underflow",
			op:   0xF4B3,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x10), int64(4), dictMatrixSubdictRoot(t), int64(8)}
			},
			exit: vmerr.CodeCellUnderflow,
		},
	}
}

func dictMatrixJumpExecCases() []dictMatrixCrossCase {
	return []dictMatrixCrossCase{
		{
			name: "jump/dictigetjmp_hit",
			op:   0xF4A0,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictMatrixSignedContRoot(t, 3), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictugetjmp_hit",
			op:   0xF4A1,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x2A), dictMatrixUnsignedContRoot(t, 4), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictigetexec_hit",
			op:   0xF4A2,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictMatrixSignedContRoot(t, 5), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictugetexec_hit",
			op:   0xF4A3,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x2A), dictMatrixUnsignedContRoot(t, 6), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictigetjmpz_hit",
			op:   0xF4BC,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictMatrixSignedContRoot(t, 7), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictugetjmpz_hit",
			op:   0xF4BD,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x2A), dictMatrixUnsignedContRoot(t, 8), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictigetexecz_hit",
			op:   0xF4BE,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), dictMatrixSignedContRoot(t, 9), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictugetexecz_hit",
			op:   0xF4BF,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x2A), dictMatrixUnsignedContRoot(t, 10), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictigetjmp_miss_drops_key",
			op:   0xF4A0,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(1), dictMatrixSignedContRoot(t, 3), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictugetexec_miss_drops_key",
			op:   0xF4A3,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x99), dictMatrixUnsignedContRoot(t, 6), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictugetjmpz_miss_keeps_key",
			op:   0xF4BD,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x99), dictMatrixUnsignedContRoot(t, 4), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictigetexecz_miss_keeps_key",
			op:   0xF4BE,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-2), dictMatrixSignedContRoot(t, 5), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictugetjmp_nil_root_miss",
			op:   0xF4A1,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(0x2A), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictigetexecz_nil_root_keeps_key",
			op:   0xF4BE,
			stack: func(t *testing.T) []any {
				return []any{big.NewInt(-1), nil, int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictugetexec_out_of_range_miss",
			op:   0xF4A3,
			stack: func(t *testing.T) []any {
				return []any{bigIntFromString(t, "1048576"), dictMatrixUnsignedContRoot(t, 6), int64(8)}
			},
			exit: 0,
		},
		{
			name: "jump/dictugetexecz_out_of_range_keeps_key",
			op:   0xF4BF,
			stack: func(t *testing.T) []any {
				return []any{bigIntFromString(t, "1048576"), dictMatrixUnsignedContRoot(t, 10), int64(8)}
			},
			exit: 0,
		},
	}
}

func runDictMatrixCrossCase(t *testing.T, code *cell.Cell, stackValues []any, exit int32) {
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

func dictMatrixOpcode(op uint16) func(*testing.T) *cell.Cell {
	return func(t *testing.T) *cell.Cell {
		t.Helper()
		return codeFromOpcodes(t, op)
	}
}

func dictMatrixSlice(value uint64, bits uint) *cell.Slice {
	return cell.BeginCell().MustStoreUInt(value, bits).EndCell().MustBeginParse()
}

func dictMatrixBuilder(value uint64, bits uint) *cell.Builder {
	return cell.BeginCell().MustStoreUInt(value, bits)
}

func dictMatrixRefValue(value uint64, bits uint) *cell.Cell {
	return cell.BeginCell().MustStoreUInt(value, bits).EndCell()
}

func dictMatrixPlainRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return mustPlainDictCell(t, 8, map[uint64]uint64{
		0x10: 0xA1,
		0x20: 0xB2,
		0x80: 0xC3,
		0xF0: 0xD4,
	}, 8)
}

func dictMatrixSignedPlainRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return mustPlainDictCell(t, 8, map[uint64]uint64{
		0x80: 0x80,
		0xFE: 0xFE,
		0xFF: 0xFF,
		0x01: 0x01,
		0x7F: 0x7F,
	}, 8)
}

func dictMatrixRefRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return dictMatrixRefDict(t, map[uint64]*cell.Cell{
		0x10: dictMatrixRefValue(0x1111, 16),
		0xFE: dictMatrixRefValue(0x2222, 16),
	})
}

func dictMatrixRefDict(t *testing.T, items map[uint64]*cell.Cell) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(8)
	for key, value := range items {
		if _, err := dict.SetBuilderWithMode(mustKeyCell(t, key, 8), cell.BeginCell().MustStoreRef(value), cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to seed ref dict: %v", err)
		}
	}
	return dict.AsCell()
}

func dictMatrixSinglePlainRoot(t *testing.T, key uint64, value uint64) *cell.Cell {
	t.Helper()
	return mustPlainDictCell(t, 8, map[uint64]uint64{key: value}, 8)
}

func dictMatrixSingleRefRoot(t *testing.T, key uint64, value uint64) *cell.Cell {
	t.Helper()
	return dictMatrixRefDict(t, map[uint64]*cell.Cell{
		key: dictMatrixRefValue(value, 16),
	})
}

func dictMatrixSignedRefRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return dictMatrixRefDict(t, map[uint64]*cell.Cell{
		0x80: dictMatrixRefValue(0x8080, 16),
		0xFF: dictMatrixRefValue(0xFFFF, 16),
		0x7F: dictMatrixRefValue(0x7F7F, 16),
	})
}

func dictMatrixUnsignedEdgeRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return mustPlainDictCell(t, 8, map[uint64]uint64{
		0x00: 0x10,
		0x01: 0x11,
		0xFE: 0xFE,
		0xFF: 0xFF,
	}, 8)
}

func dictMatrixUnsignedRefEdgeRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return dictMatrixRefDict(t, map[uint64]*cell.Cell{
		0x00: dictMatrixRefValue(0x1000, 16),
		0x01: dictMatrixRefValue(0x1100, 16),
		0xFE: dictMatrixRefValue(0xFEFE, 16),
		0xFF: dictMatrixRefValue(0xFFFF, 16),
	})
}

func dictMatrixPrefixRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
		0b10:  {bits: 2, value: dictMatrixRefValue(0xD, 4)},
		0b111: {bits: 3, value: dictMatrixRefValue(0xE, 4)},
	})
}

func dictMatrixPrefixContRoot(t *testing.T, value int64) *cell.Cell {
	t.Helper()
	return mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
		0b10: {bits: 2, value: codeFromOpcodes(t, uint16(0x70|(value&0xF)))},
	})
}

func dictMatrixSubdictRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return mustPlainDictCell(t, 8, map[uint64]uint64{
		0xA1: 0x11,
		0xA2: 0x22,
		0xB1: 0x33,
	}, 8)
}

func dictMatrixSignedSubdictRoot(t *testing.T) *cell.Cell {
	t.Helper()
	return mustPlainDictCell(t, 8, map[uint64]uint64{
		0xC0: 0x40,
		0xCF: 0x4F,
		0x20: 0x20,
	}, 8)
}

func dictMatrixSignedContRoot(t *testing.T, value int64) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(8)
	if _, err := dict.SetWithMode(mustKeyCell(t, 0xFF, 8), codeFromOpcodes(t, uint16(0x70|(value&0xF))), cell.DictSetModeSet); err != nil {
		t.Fatalf("failed to seed signed continuation dict: %v", err)
	}
	return dict.AsCell()
}

func dictMatrixUnsignedContRoot(t *testing.T, value int64) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(8)
	if _, err := dict.SetWithMode(mustKeyCell(t, 0x2A, 8), codeFromOpcodes(t, uint16(0x70|(value&0xF))), cell.DictSetModeSet); err != nil {
		t.Fatalf("failed to seed unsigned continuation dict: %v", err)
	}
	return dict.AsCell()
}
