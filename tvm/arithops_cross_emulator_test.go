//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorArithOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	minTVMInt := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))

	type testCase struct {
		name  string
		code  *cell.Cell
		stack []any
		exit  int32
	}

	tests := []testCase{
		{name: "pushpow2", code: codeFromBuilders(t, mathop.PUSHPOW2(4).Serialize()), exit: 0},
		{name: "pushpow2dec", code: codeFromBuilders(t, mathop.PUSHPOW2DEC(4).Serialize()), exit: 0},
		{name: "pushnegpow2", code: codeFromBuilders(t, mathop.PUSHNEGPOW2(4).Serialize()), exit: 0},
		{name: "pushnan_opcode", code: codeFromOpcodes(t, 0x83FF), exit: 0},
		{name: "pushpow2_max_constructor_nan", code: codeFromBuilders(t, mathop.PUSHPOW2(255).Serialize()), exit: 0},
		{name: "addint_negative", code: codeFromBuilders(t, mathop.ADDINT(-5).Serialize()), stack: []any{int64(9)}, exit: 0},
		{name: "mulint_negative", code: codeFromBuilders(t, mathop.MULINT(-3).Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "addint_type_value", code: codeFromBuilders(t, mathop.ADDINT(1).Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulint_type_value", code: codeFromBuilders(t, mathop.MULINT(2).Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "add_short_stack_nan", code: codeFromBuilders(t, mathop.SUM().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "qadd_overflow", code: codeFromBuilders(t, mathop.QADD().Serialize()), stack: []any{maxTVMInt, int64(1)}, exit: 0},
		{name: "qadd_type_rhs", code: codeFromBuilders(t, mathop.QADD().Serialize()), stack: []any{int64(1), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qadd_short_stack_null", code: codeFromBuilders(t, mathop.QADD().Serialize()), stack: []any{nil}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "qsub_nan", code: codeFromBuilders(t, mathop.QSUB().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qmul_nan", code: codeFromBuilders(t, mathop.QMUL().Serialize()), stack: []any{vm.NaN{}, int64(2)}, exit: 0},
		{name: "qsubr", code: codeFromBuilders(t, mathop.QSUBR().Serialize()), stack: []any{int64(2), int64(5)}, exit: 0},
		{name: "qnegate", code: codeFromBuilders(t, mathop.QNEGATE().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "qinc", code: codeFromBuilders(t, mathop.QINC().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "qdec", code: codeFromBuilders(t, mathop.QDEC().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "qaddint_negative", code: codeFromBuilders(t, mathop.QADDINT(-5).Serialize()), stack: []any{int64(9)}, exit: 0},
		{name: "qmulint_negative", code: codeFromBuilders(t, mathop.QMULINT(-3).Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "qaddint_type_value", code: codeFromBuilders(t, mathop.QADDINT(1).Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qsub_overflow_nan", code: codeFromBuilders(t, mathop.QSUB().Serialize()), stack: []any{minTVMInt, int64(1)}, exit: 0},
		{name: "qsubr_overflow_nan", code: codeFromBuilders(t, mathop.QSUBR().Serialize()), stack: []any{maxTVMInt, int64(-2)}, exit: 0},
		{name: "qnegate_min_overflow_nan", code: codeFromBuilders(t, mathop.QNEGATE().Serialize()), stack: []any{minTVMInt}, exit: 0},
		{name: "qinc_max_overflow_nan", code: codeFromBuilders(t, mathop.QINC().Serialize()), stack: []any{maxTVMInt}, exit: 0},
		{name: "qdec_min_overflow_nan", code: codeFromBuilders(t, mathop.QDEC().Serialize()), stack: []any{minTVMInt}, exit: 0},
		{name: "qaddint_max_overflow_nan", code: codeFromBuilders(t, mathop.QADDINT(1).Serialize()), stack: []any{maxTVMInt}, exit: 0},
		{name: "qmulint_max_overflow_nan", code: codeFromBuilders(t, mathop.QMULINT(2).Serialize()), stack: []any{maxTVMInt}, exit: 0},
		{name: "qnegate_nan", code: codeFromBuilders(t, mathop.QNEGATE().Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qaddint_nan", code: codeFromBuilders(t, mathop.QADDINT(1).Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qmulint_nan", code: codeFromBuilders(t, mathop.QMULINT(2).Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qnot_nan", code: codeFromBuilders(t, mathop.QNOT().Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qand_nan", code: codeFromBuilders(t, mathop.QAND().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qor_nan", code: codeFromBuilders(t, mathop.QOR().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qxor_nan", code: codeFromBuilders(t, mathop.QXOR().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qlshift_nan", code: codeFromBuilders(t, mathop.QLSHIFT().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qrshift_nan", code: codeFromBuilders(t, mathop.QRSHIFT().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "lshift_nan_count_range", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "lshift_short_stack_nan_count", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "lshift_type_value", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "lshift_code_type_value", code: codeFromBuilders(t, mathop.LSHIFTCODE(1).Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "pow2_underflow", code: codeFromBuilders(t, mathop.POW2().Serialize()), exit: int32(vmerr.CodeStackUnderflow)},
		{name: "pow2_nan_count_range", code: codeFromBuilders(t, mathop.POW2().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "pow2_type", code: codeFromBuilders(t, mathop.POW2().Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qpow2_overflow", code: codeFromBuilders(t, mathop.QPOW2().Serialize()), stack: []any{int64(300)}, exit: 0},
		{name: "qdiv", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A904, 24)), stack: []any{int64(10), int64(3)}, exit: 0},
		{name: "qdiv_zero_nan", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A904, 24)), stack: []any{int64(10), int64(0)}, exit: 0},
		{name: "qdiv_type_divisor", code: arithRawCode(t, 0xB7A904, 24), stack: []any{int64(10), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qdivmod", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A90C, 24)), stack: []any{int64(10), int64(3)}, exit: 0},
		{name: "divmod_type_dividend", code: codeFromBuilders(t, mathop.DIVMOD().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(3)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "adddivmod_short_stack_nan_divisor", code: codeFromBuilders(t, mathop.ADDDIVMOD().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "adddivmod_type_addend", code: codeFromBuilders(t, mathop.ADDDIVMOD().Serialize()), stack: []any{int64(5), cell.BeginCell().EndCell(), int64(2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qadddivmod_type_addend", code: arithRawCode(t, 0xB7A900, 24), stack: []any{int64(5), cell.BeginCell().EndCell(), int64(2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qaddrshiftmod", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A920, 24)), stack: []any{int64(5), int64(3), int64(1)}, exit: 0},
		{name: "qrshift_type_shift", code: arithRawCode(t, 0xB7A924, 24), stack: []any{int64(5), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qmuldivmod", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A98C, 24)), stack: []any{int64(6), int64(7), int64(5)}, exit: 0},
		{name: "qmuldivmod_short_stack_nan_divisor", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A98C, 24)), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "muldivmod_type_multiplier", code: codeFromBuilders(t, mathop.MULDIVMOD().Serialize()), stack: []any{int64(6), cell.BeginCell().EndCell(), int64(5)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qmuldiv_type_multiplier", code: arithRawCode(t, 0xB7A984, 24), stack: []any{int64(6), cell.BeginCell().EndCell(), int64(5)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "muladddivmod_type_addend", code: codeFromBuilders(t, mathop.MULADDDIVMOD().Serialize()), stack: []any{int64(6), int64(7), cell.BeginCell().EndCell(), int64(5)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qmulrshiftmod", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A9AC, 24)), stack: []any{int64(6), int64(7), int64(3)}, exit: 0},
		{name: "qmulrshiftmod_bad_shift_nan", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A9AC, 24)), stack: []any{int64(6), int64(7), int64(300)}, exit: 0},
		{name: "qmulrshift_type_multiplier", code: arithRawCode(t, 0xB7A9A4, 24), stack: []any{int64(6), cell.BeginCell().EndCell(), int64(3)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qlshiftdivmod", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A9CC, 24)), stack: []any{int64(3), int64(5), int64(2)}, exit: 0},
		{name: "qlshiftdiv_type_divisor", code: arithRawCode(t, 0xB7A9C4, 24), stack: []any{int64(3), cell.BeginCell().EndCell(), int64(2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulmod_negative_divisor", code: codeFromBuilders(t, mathop.MULMOD().Serialize()), stack: []any{int64(5), int64(1), int64(-3)}, exit: 0},
		{name: "qmulmod_negative_divisor", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A988, 24)), stack: []any{int64(5), int64(1), int64(-3)}, exit: 0},
		{name: "min_nan_top_direct", code: codeFromBuilders(t, mathop.MIN().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muldiv_nan_top_direct", code: codeFromBuilders(t, mathop.MULDIV().Serialize()), stack: []any{int64(2), int64(3), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "adddivmod_nan_middle_direct", code: codeFromBuilders(t, mathop.ADDDIVMOD().Serialize()), stack: []any{int64(5), vm.NaN{}, int64(2)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftdiv_underflow", code: codeFromBuilders(t, mathop.LSHIFTDIV().Serialize()), stack: []any{int64(5), int64(2)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "lshiftdiv_nan_divisor_direct", code: codeFromBuilders(t, mathop.LSHIFTDIV().Serialize()), stack: []any{int64(5), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftdiv_zero_divisor_direct", code: codeFromBuilders(t, mathop.LSHIFTDIV().Serialize()), stack: []any{int64(5), int64(0), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftdivc_type_value", code: codeFromBuilders(t, mathop.LSHIFTDIVC().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(2), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "lshiftmod_type_shift", code: codeFromBuilders(t, mathop.LSHIFTMOD().Serialize()), stack: []any{int64(5), int64(2), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "lshiftmodr_zero_divisor_direct", code: codeFromBuilders(t, mathop.LSHIFTMODR().Serialize()), stack: []any{int64(5), int64(0), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftmodc_nan_value_direct", code: codeFromBuilders(t, mathop.LSHIFTMODC().Serialize()), stack: []any{vm.NaN{}, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "fits_fail", code: codeFromBuilders(t, mathop.FITS(6).Serialize()), stack: []any{int64(64)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "fits_type_value", code: codeFromBuilders(t, mathop.FITS(6).Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "ufits_fail", code: codeFromBuilders(t, mathop.UFITS(6).Serialize()), stack: []any{int64(-1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "bitsize_negative", code: codeFromBuilders(t, mathop.BITSIZE().Serialize()), stack: []any{int64(-3)}, exit: 0},
		{name: "bitsize_type", code: codeFromBuilders(t, mathop.BITSIZE().Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "ubitsize_negative", code: codeFromBuilders(t, mathop.UBITSIZE().Serialize()), stack: []any{int64(-1)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "qfits_fail", code: codeFromBuilders(t, mathop.QFITS(7).Serialize()), stack: []any{int64(128)}, exit: 0},
		{name: "fitsx_fail", code: codeFromBuilders(t, mathop.FITSX().Serialize()), stack: []any{int64(128), int64(7)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "fitsx_nan_bits_range", code: codeFromBuilders(t, mathop.FITSX().Serialize()), stack: []any{int64(128), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "fitsx_short_stack_nan_bits", code: codeFromBuilders(t, mathop.FITSX().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "fitsx_type_width", code: codeFromBuilders(t, mathop.FITSX().Serialize()), stack: []any{int64(7), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "fitsx_type_value", code: codeFromBuilders(t, mathop.FITSX().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(7)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "qfitsx_fail", code: codeFromBuilders(t, mathop.QFITSX().Serialize()), stack: []any{int64(128), int64(8)}, exit: 0},
		{name: "qfitsx_nan_value", code: codeFromBuilders(t, mathop.QFITSX().Serialize()), stack: []any{vm.NaN{}, int64(7)}, exit: 0},
		{name: "qfitsx_bad_width_range", code: codeFromBuilders(t, mathop.QFITSX().Serialize()), stack: []any{int64(7), int64(1024)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "qfitsx_nan_width_range", code: codeFromBuilders(t, mathop.QFITSX().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "qufits_fail", code: codeFromBuilders(t, mathop.QUFITS(7).Serialize()), stack: []any{int64(-1)}, exit: 0},
		{name: "qufitsx_fail", code: codeFromBuilders(t, mathop.QUFITSX().Serialize()), stack: []any{int64(-1), int64(8)}, exit: 0},
		{name: "qufitsx_bad_width_range", code: codeFromBuilders(t, mathop.QUFITSX().Serialize()), stack: []any{int64(7), int64(1024)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "qbitsize_nan", code: codeFromBuilders(t, mathop.QBITSIZE().Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qubitsize_negative", code: codeFromBuilders(t, mathop.QUBITSIZE().Serialize()), stack: []any{int64(-1)}, exit: 0},
		{name: "minmax", code: codeFromBuilders(t, mathop.MINMAX().Serialize()), stack: []any{int64(5), int64(2)}, exit: 0},
		{name: "minmax_nan_top", code: codeFromBuilders(t, mathop.MINMAX().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "minmax_nan_bottom", code: codeFromBuilders(t, mathop.MINMAX().Serialize()), stack: []any{vm.NaN{}, int64(7)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "qminmax_nan", code: codeFromBuilders(t, mathop.QMINMAX().Serialize()), stack: []any{vm.NaN{}, int64(5)}, exit: 0},
		{name: "qminmax_nan_top", code: codeFromBuilders(t, mathop.QMINMAX().Serialize()), stack: []any{int64(5), vm.NaN{}}, exit: 0},
		{name: "qabs_negative", code: codeFromBuilders(t, mathop.QABS().Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "qabs_min_overflow_nan", code: codeFromBuilders(t, mathop.QABS().Serialize()), stack: []any{minTVMInt}, exit: 0},
		{name: "qabs_nan", code: codeFromBuilders(t, mathop.QABS().Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "less_order", code: codeFromBuilders(t, mathop.LESS().Serialize()), stack: []any{int64(1), int64(2)}, exit: 0},
		{name: "geq_order", code: codeFromBuilders(t, mathop.GEQ().Serialize()), stack: []any{int64(3), int64(2)}, exit: 0},
		{name: "eqint_negative", code: codeFromBuilders(t, mathop.EQINT(-5).Serialize()), stack: []any{int64(-5)}, exit: 0},
		{name: "lessint_negative", code: codeFromBuilders(t, mathop.LESSINT(-5).Serialize()), stack: []any{int64(-6)}, exit: 0},
		{name: "neqint_negative", code: codeFromBuilders(t, mathop.NEQINT(-5).Serialize()), stack: []any{int64(-4)}, exit: 0},
		{name: "sgn_negative", code: codeFromBuilders(t, mathop.SGN().Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "sgn_nan", code: codeFromBuilders(t, mathop.SGN().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "cmp_less", code: codeFromBuilders(t, mathop.CMP().Serialize()), stack: []any{int64(2), int64(5)}, exit: 0},
		{name: "cmp_nan_top", code: codeFromBuilders(t, mathop.CMP().Serialize()), stack: []any{int64(2), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "cmp_nan_bottom", code: codeFromBuilders(t, mathop.CMP().Serialize()), stack: []any{vm.NaN{}, int64(2)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "isnan_true", code: codeFromBuilders(t, mathop.ISNAN().Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "isnan_underflow", code: codeFromBuilders(t, mathop.ISNAN().Serialize()), exit: int32(vmerr.CodeStackUnderflow)},
		{name: "chknan", code: codeFromBuilders(t, mathop.CHKNAN().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "chknan_underflow", code: codeFromBuilders(t, mathop.CHKNAN().Serialize()), exit: int32(vmerr.CodeStackUnderflow)},
		{name: "qsgn_nan", code: codeFromBuilders(t, mathop.QSGN().Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qless_nan_top", code: codeFromBuilders(t, mathop.QLESS().Serialize()), stack: []any{int64(1), vm.NaN{}}, exit: 0},
		{name: "qcmp_nan", code: codeFromBuilders(t, mathop.QCMP().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qcmp_nan_top", code: codeFromBuilders(t, mathop.QCMP().Serialize()), stack: []any{int64(1), vm.NaN{}}, exit: 0},
		{name: "qeqint_nan", code: codeFromBuilders(t, mathop.QEQINT(7).Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qeqint_underflow", code: codeFromBuilders(t, mathop.QEQINT(7).Serialize()), exit: int32(vmerr.CodeStackUnderflow)},
		{name: "qgtint_negative", code: codeFromBuilders(t, mathop.QGTINT(-5).Serialize()), stack: []any{int64(-4)}, exit: 0},
		{name: "iszero_true", code: codeFromBuilders(t, mathop.ISZERO().Serialize()), stack: []any{int64(0)}, exit: 0},
		{name: "iszero_nan", code: codeFromBuilders(t, mathop.ISZERO().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "ispos_true", code: codeFromBuilders(t, mathop.ISPOS().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "ispos_underflow", code: codeFromBuilders(t, mathop.ISPOS().Serialize()), exit: int32(vmerr.CodeStackUnderflow)},
		{name: "isneg_true", code: codeFromBuilders(t, mathop.ISNEG().Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "isneg_type", code: codeFromBuilders(t, mathop.ISNEG().Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "isnneg_zero", code: codeFromBuilders(t, mathop.ISNNEG().Serialize()), stack: []any{int64(0)}, exit: 0},
		{name: "isnpos_zero", code: codeFromBuilders(t, mathop.ISNPOS().Serialize()), stack: []any{int64(0)}, exit: 0},
		{name: "rshiftfloor_negative", code: codeFromBuilders(t, mathop.RSHIFTFLOOR().Serialize()), stack: []any{int64(-7), int64(1)}, exit: 0},
		{name: "rshiftfloor_256_negative", code: codeFromBuilders(t, mathop.RSHIFTFLOOR().Serialize()), stack: []any{int64(-1), int64(256)}, exit: 0},
		{name: "rshiftfloor_257_range", code: codeFromBuilders(t, mathop.RSHIFTFLOOR().Serialize()), stack: []any{int64(7), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "rshiftfloor_nan_shift_range", code: codeFromBuilders(t, mathop.RSHIFTFLOOR().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "rshiftfloor_nan_value_shift0_overflow", code: codeFromBuilders(t, mathop.RSHIFTFLOOR().Serialize()), stack: []any{vm.NaN{}, int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshiftfloor_nan_value_zero", code: codeFromBuilders(t, mathop.RSHIFTFLOOR().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "rshiftfloor_nan_value_shift13_minus_one", code: codeFromBuilders(t, mathop.RSHIFTFLOOR().Serialize()), stack: []any{vm.NaN{}, int64(13)}, exit: 0},
		{name: "rshiftr_nan_value_shift0_overflow", code: codeFromBuilders(t, mathop.RSHIFTR().Serialize()), stack: []any{vm.NaN{}, int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshiftr_nan_value_zero", code: codeFromBuilders(t, mathop.RSHIFTR().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "rshiftr_nan_value_shift13_zero", code: codeFromBuilders(t, mathop.RSHIFTR().Serialize()), stack: []any{vm.NaN{}, int64(13)}, exit: 0},
		{name: "rshiftc_nan_value_shift0_overflow", code: codeFromBuilders(t, mathop.RSHIFTC().Serialize()), stack: []any{vm.NaN{}, int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshiftc_nan_value_zero", code: codeFromBuilders(t, mathop.RSHIFTC().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "rshiftc_nan_value_shift13_zero", code: codeFromBuilders(t, mathop.RSHIFTC().Serialize()), stack: []any{vm.NaN{}, int64(13)}, exit: 0},
		{name: "rshift_underflow", code: codeFromBuilders(t, mathop.RSHIFT().Serialize()), stack: []any{int64(7)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "rshift_type_shift", code: codeFromBuilders(t, mathop.RSHIFT().Serialize()), stack: []any{int64(7), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "rshift_type_value", code: codeFromBuilders(t, mathop.RSHIFT().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "rshiftcodefloor_negative", code: codeFromBuilders(t, mathop.RSHIFTCODEFLOOR(1).Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "rshiftcodefloor_256_negative", code: arithRawCode(t, 0xa934ff, 24), stack: []any{int64(-1)}, exit: 0},
		{name: "rshiftcodefloor_nan_value_zero", code: codeFromBuilders(t, mathop.RSHIFTCODEFLOOR(1).Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "rshiftcodefloor_nan_value_shift13_minus_one", code: arithRawCode(t, 0xa9340c, 24), stack: []any{vm.NaN{}}, exit: 0},
		{name: "rshiftr_code_nan_value_zero", code: codeFromBuilders(t, mathop.RSHIFTRCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "rshiftr_code_nan_value_shift13_zero", code: arithRawCode(t, 0xa9350c, 24), stack: []any{vm.NaN{}}, exit: 0},
		{name: "rshiftc_code_nan_value_zero", code: codeFromBuilders(t, mathop.RSHIFTCCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "rshiftc_code_nan_value_shift13_zero", code: arithRawCode(t, 0xa9360c, 24), stack: []any{vm.NaN{}}, exit: 0},
		{name: "rshift_code_underflow", code: codeFromBuilders(t, mathop.RSHIFTCODE(1).Serialize()), exit: int32(vmerr.CodeStackUnderflow)},
		{name: "rshift_code_type", code: codeFromBuilders(t, mathop.RSHIFTCODE(1).Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "rshiftr_code_type", code: codeFromBuilders(t, mathop.RSHIFTRCODE(1).Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "rshiftc_code_type", code: codeFromBuilders(t, mathop.RSHIFTCCODE(1).Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "modpow2_code_nan_value_overflow", code: codeFromBuilders(t, mathop.MODPOW2CODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "modpow2r_code_nan_value_overflow", code: codeFromBuilders(t, mathop.MODPOW2RCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "modpow2c_code_nan_value_overflow", code: codeFromBuilders(t, mathop.MODPOW2CCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshift_code_mod_nan_value_overflow", code: codeFromBuilders(t, mathop.RSHIFTCODEMOD(1).Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshift_code_mod_type_value", code: codeFromBuilders(t, mathop.RSHIFTCODEMOD(1).Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "rshiftr_code_mod_nan_value_overflow", code: codeFromBuilders(t, mathop.RSHIFTRCODEMOD(1).Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshiftc_code_mod_nan_value_overflow", code: codeFromBuilders(t, mathop.RSHIFTCCODEMOD(1).Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "modpow2_nan_value_shift0_overflow", code: codeFromBuilders(t, mathop.MODPOW2().Serialize()), stack: []any{vm.NaN{}, int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "modpow2_nan_value_shift1_overflow", code: codeFromBuilders(t, mathop.MODPOW2().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "modpow2r_nan_value_shift1_overflow", code: codeFromBuilders(t, mathop.MODPOW2R().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "modpow2c_nan_value_shift1_overflow", code: codeFromBuilders(t, mathop.MODPOW2C().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshiftmod_nan_value_shift0_overflow", code: codeFromBuilders(t, mathop.RSHIFTMOD().Serialize()), stack: []any{vm.NaN{}, int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshiftmod_nan_value_shift1_overflow", code: codeFromBuilders(t, mathop.RSHIFTMOD().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshiftmodr_nan_value_shift1_overflow", code: codeFromBuilders(t, mathop.RSHIFTMODR().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshiftmodc_nan_value_shift1_overflow", code: codeFromBuilders(t, mathop.RSHIFTMODC().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "rshiftmod_nan_shift_range", code: codeFromBuilders(t, mathop.RSHIFTMOD().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "modpow2_nan_shift_range", code: codeFromBuilders(t, mathop.MODPOW2().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "mulmodpow2_nan_x_shift1_overflow", code: codeFromBuilders(t, mathop.MULMODPOW2_VAR().Serialize()), stack: []any{vm.NaN{}, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulmodpow2_nan_y_shift1_overflow", code: codeFromBuilders(t, mathop.MULMODPOW2_VAR().Serialize()), stack: []any{int64(2), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulmodpow2r_nan_x_shift1_overflow", code: codeFromBuilders(t, mathop.MULMODPOW2R_VAR().Serialize()), stack: []any{vm.NaN{}, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulmodpow2c_nan_y_shift1_overflow", code: codeFromBuilders(t, mathop.MULMODPOW2C_VAR().Serialize()), stack: []any{int64(2), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulmodpow2_nan_shift_range", code: codeFromBuilders(t, mathop.MULMODPOW2_VAR().Serialize()), stack: []any{int64(2), int64(3), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "mulmodpow2_type_shift", code: codeFromBuilders(t, mathop.MULMODPOW2_VAR().Serialize()), stack: []any{int64(2), int64(3), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulmodpow2r_type_multiplier", code: codeFromBuilders(t, mathop.MULMODPOW2R_VAR().Serialize()), stack: []any{int64(2), cell.BeginCell().EndCell(), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulmodpow2_257_range", code: codeFromBuilders(t, mathop.MULMODPOW2_VAR().Serialize()), stack: []any{int64(2), int64(3), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "mulrshift_underflow", code: codeFromBuilders(t, mathop.MULRSHIFT().Serialize()), stack: []any{int64(1), int64(2)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "mulrshift_nan_shift_range", code: codeFromBuilders(t, mathop.MULRSHIFT().Serialize()), stack: []any{int64(2), int64(3), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "mulrshift_257_range", code: codeFromBuilders(t, mathop.MULRSHIFT().Serialize()), stack: []any{int64(2), int64(3), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "mulrshiftr_nan_y_shift1_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTR().Serialize()), stack: []any{int64(2), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftc_nan_x_shift1_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTC().Serialize()), stack: []any{vm.NaN{}, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshift_wide_product_shift1_ok", code: codeFromBuilders(t, mathop.MULRSHIFT().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: 0},
		{name: "mulrshift_wide_product_shift0_overflow", code: codeFromBuilders(t, mathop.MULRSHIFT().Serialize()), stack: []any{maxTVMInt, int64(2), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftmod_nan_x_shift1_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTMOD_VAR().Serialize()), stack: []any{vm.NaN{}, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftmodr_nan_x_shift1_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTRMOD_VAR().Serialize()), stack: []any{vm.NaN{}, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftmodc_nan_y_shift1_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTCMOD_VAR().Serialize()), stack: []any{int64(2), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftmod_nan_shift_range", code: codeFromBuilders(t, mathop.MULRSHIFTMOD_VAR().Serialize()), stack: []any{int64(2), int64(3), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "mulrshiftmod_type_multiplier", code: codeFromBuilders(t, mathop.MULRSHIFTMOD_VAR().Serialize()), stack: []any{int64(2), cell.BeginCell().EndCell(), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulrshiftmod_257_range", code: codeFromBuilders(t, mathop.MULRSHIFTMOD_VAR().Serialize()), stack: []any{int64(2), int64(3), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "mulmodpow2_wide_product_shift0_zero", code: codeFromBuilders(t, mathop.MULMODPOW2_VAR().Serialize()), stack: []any{maxTVMInt, int64(2), int64(0)}, exit: 0},
		{name: "mulrshiftmod_wide_product_shift0_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTMOD_VAR().Serialize()), stack: []any{maxTVMInt, int64(2), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulmodpow2_code_nan_x_overflow", code: codeFromBuilders(t, mathop.MULMODPOW2CODE(0).Serialize()), stack: []any{vm.NaN{}, int64(2)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulmodpow2r_code_nan_y_overflow", code: codeFromBuilders(t, mathop.MULMODPOW2RCODE(0).Serialize()), stack: []any{int64(2), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulmodpow2c_code_wide_product_shift1_ok", code: codeFromBuilders(t, mathop.MULMODPOW2CCODE(0).Serialize()), stack: []any{maxTVMInt, int64(3)}, exit: 0},
		{name: "mulmodpow2_code_underflow", code: codeFromBuilders(t, mathop.MULMODPOW2CODE(0).Serialize()), stack: []any{int64(2)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "mulmodpow2_code_type_top", code: codeFromBuilders(t, mathop.MULMODPOW2CODE(0).Serialize()), stack: []any{int64(2), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulmodpow2r_code_type_bottom", code: codeFromBuilders(t, mathop.MULMODPOW2RCODE(0).Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulmodpow2c_code_nan_y_overflow", code: codeFromBuilders(t, mathop.MULMODPOW2CCODE(0).Serialize()), stack: []any{int64(2), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulmodpow2r_code_negative_round", code: codeFromBuilders(t, mathop.MULMODPOW2RCODE(2).Serialize()), stack: []any{int64(-5), int64(3)}, exit: 0},
		{name: "mulmodpow2c_code_negative_ceil", code: codeFromBuilders(t, mathop.MULMODPOW2CCODE(2).Serialize()), stack: []any{int64(-5), int64(3)}, exit: 0},
		{name: "mulrshift_code_underflow", code: codeFromBuilders(t, mathop.MULRSHIFTCODE(0).Serialize()), stack: []any{int64(2)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "mulrshift_code_nan_x_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTCODE(0).Serialize()), stack: []any{vm.NaN{}, int64(2)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftr_code_nan_y_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTRCODE(0).Serialize()), stack: []any{int64(2), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftc_code_wide_product_shift1_ok", code: codeFromBuilders(t, mathop.MULRSHIFTCCODE(0).Serialize()), stack: []any{maxTVMInt, int64(2)}, exit: 0},
		{name: "mulrshiftc_code_wide_product_shift1_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTCCODE(0).Serialize()), stack: []any{maxTVMInt, int64(3)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftmod_code_nan_x_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTCODEMOD(0).Serialize()), stack: []any{vm.NaN{}, int64(2)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftmodr_code_nan_y_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTRCODEMOD(0).Serialize()), stack: []any{int64(2), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftmodc_code_wide_product_shift1_overflow", code: codeFromBuilders(t, mathop.MULRSHIFTCCODEMOD(0).Serialize()), stack: []any{maxTVMInt, int64(3)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshiftmod_code_underflow", code: codeFromBuilders(t, mathop.MULRSHIFTCODEMOD(0).Serialize()), stack: []any{int64(2)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "mulrshiftmod_code_type_top", code: codeFromBuilders(t, mathop.MULRSHIFTCODEMOD(0).Serialize()), stack: []any{int64(2), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulrshiftmodr_code_type_bottom", code: codeFromBuilders(t, mathop.MULRSHIFTRCODEMOD(0).Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulrshiftmodr_code_negative_round", code: codeFromBuilders(t, mathop.MULRSHIFTRCODEMOD(2).Serialize()), stack: []any{int64(-5), int64(3)}, exit: 0},
		{name: "mulrshiftmodc_code_negative_ceil", code: codeFromBuilders(t, mathop.MULRSHIFTCCODEMOD(2).Serialize()), stack: []any{int64(-5), int64(3)}, exit: 0},
		{name: "rshiftc_256_boundary", code: codeFromBuilders(t, mathop.RSHIFTC().Serialize()), stack: []any{int64(7), int64(256)}, exit: 0},
		{name: "rshiftc_257_range", code: codeFromBuilders(t, mathop.RSHIFTC().Serialize()), stack: []any{int64(7), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "modpow2_257_range", code: codeFromBuilders(t, mathop.MODPOW2().Serialize()), stack: []any{int64(7), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "rshiftmod_257_range", code: codeFromBuilders(t, mathop.RSHIFTMOD().Serialize()), stack: []any{int64(7), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "div_min_negative_one_overflow", code: codeFromBuilders(t, mathop.DIV().Serialize()), stack: []any{minTVMInt, int64(-1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "divr_min_negative_one_overflow", code: codeFromBuilders(t, mathop.DIVR().Serialize()), stack: []any{minTVMInt, int64(-1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "divc_min_negative_one_overflow", code: codeFromBuilders(t, mathop.DIVC().Serialize()), stack: []any{minTVMInt, int64(-1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mod_min_negative_one_zero", code: codeFromBuilders(t, mathop.MOD().Serialize()), stack: []any{minTVMInt, int64(-1)}, exit: 0},
		{name: "modr_min_negative_one_zero", code: codeFromBuilders(t, mathop.MODR().Serialize()), stack: []any{minTVMInt, int64(-1)}, exit: 0},
		{name: "modc_min_negative_one_zero", code: codeFromBuilders(t, mathop.MODC().Serialize()), stack: []any{minTVMInt, int64(-1)}, exit: 0},
		{name: "div_underflow", code: codeFromBuilders(t, mathop.DIV().Serialize()), stack: []any{int64(7)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "div_type_divisor", code: codeFromBuilders(t, mathop.DIV().Serialize()), stack: []any{int64(7), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "divr_type_value", code: codeFromBuilders(t, mathop.DIVR().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mod_underflow", code: codeFromBuilders(t, mathop.MOD().Serialize()), stack: []any{int64(7)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "mod_type_divisor", code: codeFromBuilders(t, mathop.MOD().Serialize()), stack: []any{int64(7), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "modc_type_value", code: codeFromBuilders(t, mathop.MODC().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "divmodc_quotient_overflow", code: codeFromBuilders(t, mathop.DIVMODC().Serialize()), stack: []any{minTVMInt, int64(-1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "divmod_quotient_overflow", code: codeFromBuilders(t, mathop.DIVMOD().Serialize()), stack: []any{minTVMInt, int64(-1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "divmodr_quotient_overflow", code: codeFromBuilders(t, mathop.DIVMODR().Serialize()), stack: []any{minTVMInt, int64(-1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muldiv_underflow", code: codeFromBuilders(t, mathop.MULDIV().Serialize()), stack: []any{int64(5), int64(2)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "muldiv_type_divisor", code: codeFromBuilders(t, mathop.MULDIV().Serialize()), stack: []any{int64(5), int64(2), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "muldivr_type_multiplier", code: codeFromBuilders(t, mathop.MULDIVR().Serialize()), stack: []any{int64(5), cell.BeginCell().EndCell(), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "muldivc_type_value", code: codeFromBuilders(t, mathop.MULDIVC().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(2), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulmod_underflow", code: codeFromBuilders(t, mathop.MULMOD().Serialize()), stack: []any{int64(5), int64(2)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "mulmod_type_divisor", code: codeFromBuilders(t, mathop.MULMOD().Serialize()), stack: []any{int64(5), int64(2), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulmodr_type_multiplier", code: codeFromBuilders(t, mathop.MULMODR().Serialize()), stack: []any{int64(5), cell.BeginCell().EndCell(), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mulmodc_type_value", code: codeFromBuilders(t, mathop.MULMODC().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(2), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "adddivmod_zero_divisor", code: codeFromBuilders(t, mathop.ADDDIVMOD().Serialize()), stack: []any{int64(5), int64(1), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "adddivmod_nan_divisor_overflow", code: codeFromBuilders(t, mathop.ADDDIVMOD().Serialize()), stack: []any{int64(5), int64(1), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "adddivmod_wide_sum_overflow", code: codeFromBuilders(t, mathop.ADDDIVMOD().Serialize()), stack: []any{maxTVMInt, int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "adddivmodr_wide_sum_overflow", code: codeFromBuilders(t, mathop.ADDDIVMODR().Serialize()), stack: []any{maxTVMInt, int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "adddivmodc_wide_sum_overflow", code: codeFromBuilders(t, mathop.ADDDIVMODC().Serialize()), stack: []any{maxTVMInt, int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muldivmod_wide_product_valid_result", code: codeFromBuilders(t, mathop.MULDIVMOD().Serialize()), stack: []any{maxTVMInt, int64(2), int64(2)}, exit: 0},
		{name: "muldivmod_wide_product_overflow_result", code: codeFromBuilders(t, mathop.MULDIVMOD().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muldivmodr_wide_product_overflow_result", code: codeFromBuilders(t, mathop.MULDIVMODR().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muldivmodc_wide_product_overflow_result", code: codeFromBuilders(t, mathop.MULDIVMODC().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muldivr_wide_product_valid_result", code: codeFromBuilders(t, mathop.MULDIVR().Serialize()), stack: []any{maxTVMInt, int64(2), int64(2)}, exit: 0},
		{name: "muldivc_wide_product_valid_result", code: codeFromBuilders(t, mathop.MULDIVC().Serialize()), stack: []any{maxTVMInt, int64(2), int64(2)}, exit: 0},
		{name: "muldivr_wide_product_overflow_result", code: codeFromBuilders(t, mathop.MULDIVR().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muldivc_wide_product_overflow_result", code: codeFromBuilders(t, mathop.MULDIVC().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muldiv_nan_multiplier_overflow", code: codeFromBuilders(t, mathop.MULDIV().Serialize()), stack: []any{int64(5), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muldiv_zero_divisor", code: codeFromBuilders(t, mathop.MULDIV().Serialize()), stack: []any{int64(5), int64(2), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulmod_wide_product_divisor_one_zero", code: codeFromBuilders(t, mathop.MULMOD().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: 0},
		{name: "mulmodr_wide_product_divisor_one_zero", code: codeFromBuilders(t, mathop.MULMODR().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: 0},
		{name: "mulmodc_wide_product_divisor_one_zero", code: codeFromBuilders(t, mathop.MULMODC().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: 0},
		{name: "mulmod_nan_divisor_overflow", code: codeFromBuilders(t, mathop.MULMOD().Serialize()), stack: []any{int64(5), int64(2), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulmod_zero_divisor", code: codeFromBuilders(t, mathop.MULMOD().Serialize()), stack: []any{int64(5), int64(2), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftdiv_nan_shift_range", code: codeFromBuilders(t, mathop.LSHIFTDIV().Serialize()), stack: []any{int64(5), int64(2), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "lshiftdiv_257_range_beats_nan_value", code: codeFromBuilders(t, mathop.LSHIFTDIV().Serialize()), stack: []any{vm.NaN{}, int64(2), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "lshiftdiv_wide_shift_overflow", code: codeFromBuilders(t, mathop.LSHIFTDIV().Serialize()), stack: []any{maxTVMInt, int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftmod_wide_shift_divisor_one_zero", code: codeFromBuilders(t, mathop.LSHIFTMOD().Serialize()), stack: []any{maxTVMInt, int64(1), int64(1)}, exit: 0},
		{name: "lshiftdivmod_wide_shift_overflow", code: codeFromBuilders(t, mathop.LSHIFTDIVMOD().Serialize()), stack: []any{maxTVMInt, int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladddivmod_nan_x_overflow", code: codeFromBuilders(t, mathop.MULADDDIVMOD().Serialize()), stack: []any{vm.NaN{}, int64(2), int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladddivmod_nan_y_overflow", code: codeFromBuilders(t, mathop.MULADDDIVMOD().Serialize()), stack: []any{int64(5), vm.NaN{}, int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladddivmod_nan_w_overflow", code: codeFromBuilders(t, mathop.MULADDDIVMOD().Serialize()), stack: []any{int64(5), int64(2), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladddivmod_nan_divisor_overflow", code: codeFromBuilders(t, mathop.MULADDDIVMOD().Serialize()), stack: []any{int64(5), int64(2), int64(1), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladddivmod_zero_divisor", code: codeFromBuilders(t, mathop.MULADDDIVMOD().Serialize()), stack: []any{int64(5), int64(2), int64(1), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladddivmod_wide_valid", code: codeFromBuilders(t, mathop.MULADDDIVMOD().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1), int64(2)}, exit: 0},
		{name: "muladddivmod_wide_overflow", code: codeFromBuilders(t, mathop.MULADDDIVMOD().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladddivmodr_wide_overflow", code: codeFromBuilders(t, mathop.MULADDDIVMODR().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladddivmodc_wide_overflow", code: codeFromBuilders(t, mathop.MULADDDIVMODC().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftadddivmod_nan_shift_range", code: codeFromBuilders(t, mathop.LSHIFTADDDIVMOD().Serialize()), stack: []any{int64(5), int64(1), int64(2), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "lshiftadddivmod_257_range_beats_nan_value", code: codeFromBuilders(t, mathop.LSHIFTADDDIVMOD().Serialize()), stack: []any{vm.NaN{}, int64(1), int64(2), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "lshiftadddivmod_nan_divisor_overflow", code: codeFromBuilders(t, mathop.LSHIFTADDDIVMOD().Serialize()), stack: []any{int64(5), int64(1), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftadddivmod_zero_divisor", code: codeFromBuilders(t, mathop.LSHIFTADDDIVMOD().Serialize()), stack: []any{int64(5), int64(1), int64(0), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftadddivmod_wide_valid", code: codeFromBuilders(t, mathop.LSHIFTADDDIVMOD().Serialize()), stack: []any{maxTVMInt, int64(1), int64(2), int64(1)}, exit: 0},
		{name: "lshiftadddivmod_wide_overflow", code: codeFromBuilders(t, mathop.LSHIFTADDDIVMOD().Serialize()), stack: []any{maxTVMInt, int64(1), int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftadddivmodr_wide_overflow", code: codeFromBuilders(t, mathop.LSHIFTADDDIVMODR().Serialize()), stack: []any{maxTVMInt, int64(1), int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftadddivmodc_wide_overflow", code: codeFromBuilders(t, mathop.LSHIFTADDDIVMODC().Serialize()), stack: []any{maxTVMInt, int64(1), int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "addrshiftmod_nan_x_overflow", code: codeFromBuilders(t, mathop.ADDRSHIFTMOD().Serialize()), stack: []any{vm.NaN{}, int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "addrshiftmod_nan_w_overflow", code: codeFromBuilders(t, mathop.ADDRSHIFTMOD().Serialize()), stack: []any{int64(5), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "addrshiftmod_nan_shift_range", code: codeFromBuilders(t, mathop.ADDRSHIFTMOD().Serialize()), stack: []any{int64(5), int64(1), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "addrshiftmod_257_range_beats_nan_value", code: codeFromBuilders(t, mathop.ADDRSHIFTMOD().Serialize()), stack: []any{vm.NaN{}, int64(1), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "addrshiftmod_wide_valid", code: codeFromBuilders(t, mathop.ADDRSHIFTMOD().Serialize()), stack: []any{maxTVMInt, int64(1), int64(1)}, exit: 0},
		{name: "addrshiftmod_wide_overflow", code: codeFromBuilders(t, mathop.ADDRSHIFTMOD().Serialize()), stack: []any{maxTVMInt, int64(1), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "addrshiftmodr_wide_overflow", code: codeFromBuilders(t, mathop.ADDRSHIFTMODR().Serialize()), stack: []any{maxTVMInt, int64(1), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "addrshiftmodc_wide_overflow", code: codeFromBuilders(t, mathop.ADDRSHIFTMODC().Serialize()), stack: []any{maxTVMInt, int64(1), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshiftmod_nan_x_overflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTMOD().Serialize()), stack: []any{vm.NaN{}, int64(2), int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshiftmod_nan_y_overflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTMOD().Serialize()), stack: []any{int64(5), vm.NaN{}, int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshiftmod_nan_w_overflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTMOD().Serialize()), stack: []any{int64(5), int64(2), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshiftmod_nan_shift_range", code: codeFromBuilders(t, mathop.MULADDRSHIFTMOD().Serialize()), stack: []any{int64(5), int64(2), int64(1), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "addrshiftmod_type_shift", code: codeFromBuilders(t, mathop.ADDRSHIFTMOD().Serialize()), stack: []any{int64(5), int64(2), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "muladdrshiftmod_type_addend", code: codeFromBuilders(t, mathop.MULADDRSHIFTMOD().Serialize()), stack: []any{int64(5), int64(2), cell.BeginCell().EndCell(), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "muladdrshiftmod_257_range_beats_nan_value", code: codeFromBuilders(t, mathop.MULADDRSHIFTMOD().Serialize()), stack: []any{vm.NaN{}, int64(2), int64(1), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "muladdrshiftmod_wide_valid", code: codeFromBuilders(t, mathop.MULADDRSHIFTMOD().Serialize()), stack: []any{maxTVMInt, int64(2), int64(0), int64(1)}, exit: 0},
		{name: "muladdrshiftmod_wide_overflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTMOD().Serialize()), stack: []any{maxTVMInt, int64(2), int64(0), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshiftmodr_wide_overflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTRMOD().Serialize()), stack: []any{maxTVMInt, int64(2), int64(0), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshiftmodc_wide_overflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTCMOD().Serialize()), stack: []any{maxTVMInt, int64(2), int64(0), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "addrshift_code_mod_underflow", code: codeFromBuilders(t, mathop.ADDRSHIFTCODEMOD(0).Serialize()), stack: []any{int64(1)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "addrshift_code_mod_type_top", code: codeFromBuilders(t, mathop.ADDRSHIFTCODEMOD(0).Serialize()), stack: []any{int64(1), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "addrshift_code_mod_type_bottom", code: codeFromBuilders(t, mathop.ADDRSHIFTCODEMOD(0).Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "addrshift_code_mod_nan_top", code: codeFromBuilders(t, mathop.ADDRSHIFTCODEMOD(0).Serialize()), stack: []any{int64(1), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "addrshiftr_code_mod_nan_bottom", code: codeFromBuilders(t, mathop.ADDRSHIFTRCODEMOD(0).Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "addrshift_code_mod_wide_valid", code: codeFromBuilders(t, mathop.ADDRSHIFTCODEMOD(0).Serialize()), stack: []any{maxTVMInt, maxTVMInt}, exit: 0},
		{name: "addrshiftr_code_mod_round_negative", code: codeFromBuilders(t, mathop.ADDRSHIFTRCODEMOD(3).Serialize()), stack: []any{int64(-9), int64(4)}, exit: 0},
		{name: "addrshiftc_code_mod_ceil_negative", code: codeFromBuilders(t, mathop.ADDRSHIFTCCODEMOD(3).Serialize()), stack: []any{int64(-9), int64(4)}, exit: 0},
		{name: "muladdrshift_code_mod_underflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTCODEMOD(0).Serialize()), stack: []any{int64(5), int64(2)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "muladdrshift_code_mod_type_top", code: codeFromBuilders(t, mathop.MULADDRSHIFTCODEMOD(0).Serialize()), stack: []any{int64(5), int64(2), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "muladdrshiftr_code_mod_type_middle", code: codeFromBuilders(t, mathop.MULADDRSHIFTRCODEMOD(0).Serialize()), stack: []any{int64(5), cell.BeginCell().EndCell(), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "muladdrshiftc_code_mod_nan_bottom", code: codeFromBuilders(t, mathop.MULADDRSHIFTCCODEMOD(0).Serialize()), stack: []any{vm.NaN{}, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshift_code_mod_wide_valid", code: codeFromBuilders(t, mathop.MULADDRSHIFTCODEMOD(0).Serialize()), stack: []any{maxTVMInt, int64(2), int64(0)}, exit: 0},
		{name: "muladdrshift_code_mod_wide_overflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTCODEMOD(0).Serialize()), stack: []any{maxTVMInt, int64(3), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshiftr_code_mod_wide_overflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTRCODEMOD(0).Serialize()), stack: []any{maxTVMInt, int64(3), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshiftc_code_mod_wide_overflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTCCODEMOD(0).Serialize()), stack: []any{maxTVMInt, int64(3), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshiftc_code_mod_negative", code: codeFromBuilders(t, mathop.MULADDRSHIFTCCODEMOD(2).Serialize()), stack: []any{int64(-5), int64(3), int64(1)}, exit: 0},
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

const arithSignedRoundingEdgeCaseCount = 56

func arithSignedRoundingEdgeCases(t *testing.T) []arithParityCase {
	t.Helper()

	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	largePositive := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(3))
	largeNegative := new(big.Int).Neg(new(big.Int).Set(largePositive))

	return []arithParityCase{
		{name: "div_floor_negative_divisor", code: codeFromBuilders(t, mathop.DIV().Serialize()), stack: []any{int64(5), int64(-3)}, exit: 0},
		{name: "div_floor_negative_dividend", code: codeFromBuilders(t, mathop.DIV().Serialize()), stack: []any{int64(-5), int64(3)}, exit: 0},
		{name: "divr_tie_positive", code: codeFromBuilders(t, mathop.DIVR().Serialize()), stack: []any{int64(5), int64(2)}, exit: 0},
		{name: "divr_tie_negative_dividend", code: codeFromBuilders(t, mathop.DIVR().Serialize()), stack: []any{int64(-5), int64(2)}, exit: 0},
		{name: "divr_tie_negative_divisor", code: codeFromBuilders(t, mathop.DIVR().Serialize()), stack: []any{int64(5), int64(-2)}, exit: 0},
		{name: "divc_negative_divisor", code: codeFromBuilders(t, mathop.DIVC().Serialize()), stack: []any{int64(5), int64(-3)}, exit: 0},
		{name: "divmodc_negative_dividend", code: codeFromBuilders(t, mathop.DIVMODC().Serialize()), stack: []any{int64(-5), int64(3)}, exit: 0},
		{name: "modc_negative_divisor", code: codeFromBuilders(t, mathop.MODC().Serialize()), stack: []any{int64(5), int64(-3)}, exit: 0},
		{name: "qdivc_negative_divisor", code: arithRawCode(t, 0xb7a906, 24), stack: []any{int64(5), int64(-3)}, exit: 0},
		{name: "qdivmodr_tie_negative_dividend", code: arithRawCode(t, 0xb7a90d, 24), stack: []any{int64(-5), int64(2)}, exit: 0},

		{name: "rshiftr_tie_negative", code: codeFromBuilders(t, mathop.RSHIFTR().Serialize()), stack: []any{int64(-5), int64(1)}, exit: 0},
		{name: "rshiftc_negative", code: codeFromBuilders(t, mathop.RSHIFTC().Serialize()), stack: []any{int64(-5), int64(1)}, exit: 0},
		{name: "modpow2r_tie_negative", code: codeFromBuilders(t, mathop.MODPOW2R().Serialize()), stack: []any{int64(-5), int64(1)}, exit: 0},
		{name: "modpow2c_negative", code: codeFromBuilders(t, mathop.MODPOW2C().Serialize()), stack: []any{int64(-5), int64(1)}, exit: 0},
		{name: "rshiftr_code_tie_negative", code: codeFromBuilders(t, mathop.RSHIFTRCODE(1).Serialize()), stack: []any{int64(-5)}, exit: 0},
		{name: "rshiftc_code_mod_negative", code: codeFromBuilders(t, mathop.RSHIFTCCODEMOD(1).Serialize()), stack: []any{int64(-5)}, exit: 0},

		{name: "adddivmodr_negative_divisor", code: codeFromBuilders(t, mathop.ADDDIVMODR().Serialize()), stack: []any{int64(5), int64(2), int64(-3)}, exit: 0},
		{name: "muldivr_negative_product", code: codeFromBuilders(t, mathop.MULDIVR().Serialize()), stack: []any{int64(-5), int64(3), int64(2)}, exit: 0},
		{name: "mulmodc_negative_divisor", code: codeFromBuilders(t, mathop.MULMODC().Serialize()), stack: []any{int64(5), int64(3), int64(-4)}, exit: 0},
		{name: "mulrshiftrmod_tie_negative", code: codeFromBuilders(t, mathop.MULRSHIFTRMOD_VAR().Serialize()), stack: []any{int64(-5), int64(3), int64(2)}, exit: 0},
		{name: "mulrshiftr_code_mod_tie_negative", code: codeFromBuilders(t, mathop.MULRSHIFTRCODEMOD(2).Serialize()), stack: []any{int64(-5), int64(3)}, exit: 0},
		{name: "lshiftdivr_negative_divisor", code: codeFromBuilders(t, mathop.LSHIFTDIVR().Serialize()), stack: []any{int64(5), int64(-3), int64(1)}, exit: 0},
		{name: "lshiftadddivmodc_negative_dividend", code: codeFromBuilders(t, mathop.LSHIFTADDDIVMODC().Serialize()), stack: []any{int64(-5), int64(1), int64(3), int64(1)}, exit: 0},

		{name: "adddivmod_wide_sum_valid_result", code: codeFromBuilders(t, mathop.ADDDIVMOD().Serialize()), stack: []any{maxTVMInt, int64(1), int64(2)}, exit: 0},
		{name: "muldiv_wide_product_valid_result", code: codeFromBuilders(t, mathop.MULDIV().Serialize()), stack: []any{maxTVMInt, int64(2), int64(2)}, exit: 0},
		{name: "lshiftdiv_wide_shift_valid_result", code: codeFromBuilders(t, mathop.LSHIFTDIV().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: 0},
		{name: "muldiv_wide_product_overflow_result", code: codeFromBuilders(t, mathop.MULDIV().Serialize()), stack: []any{maxTVMInt, int64(2), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "mulrshift_large_negative_round", code: codeFromBuilders(t, mathop.MULRSHIFTR().Serialize()), stack: []any{largeNegative, int64(3), int64(2)}, exit: 0},
		{name: "mulrshift_large_positive_ceil", code: codeFromBuilders(t, mathop.MULRSHIFTC().Serialize()), stack: []any{largePositive, int64(3), int64(2)}, exit: 0},
		{name: "pow2_256_overflow", code: codeFromBuilders(t, mathop.POW2().Serialize()), stack: []any{int64(256)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshift_zero_259", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{int64(0), int64(259)}, exit: 0},
		{name: "lshift_zero_260_overflow", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{int64(0), int64(260)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshift_max_overflow", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{maxTVMInt, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "qlshift_max_overflow_nan", code: codeFromBuilders(t, mathop.QLSHIFT().Serialize()), stack: []any{maxTVMInt, int64(1)}, exit: 0},
		{name: "qlshift_zero_260_nan", code: codeFromBuilders(t, mathop.QLSHIFT().Serialize()), stack: []any{int64(0), int64(260)}, exit: 0},
		{name: "qlshiftdivmod_bad_shift_nan", code: arithRawCode(t, 0xb7a9cc, 24), stack: []any{int64(3), int64(5), int64(257)}, exit: 0},
		{name: "q_rshiftmod_code_unregistered", code: arithRawCode(t, 0xb7a93c02, 32), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "q_mulrshiftmod_code_unregistered", code: arithRawCode(t, 0xb7a9bc02, 32), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "q_lshiftdivmod_code_unregistered", code: arithRawCode(t, 0xb7a9dc02, 32), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "qdivmod_invalid_round", code: arithRawCode(t, 0xb7a903, 24), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "qrshiftmod_invalid_round", code: arithRawCode(t, 0xb7a923, 24), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "qmuldivmod_invalid_round", code: arithRawCode(t, 0xb7a983, 24), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "qmulrshiftmod_invalid_round", code: arithRawCode(t, 0xb7a9a3, 24), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "qlshiftdivmod_invalid_round", code: arithRawCode(t, 0xb7a9c3, 24), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "lshiftadddivmod_code_underflow", code: arithRawCode(t, 0xa9d000, 24), stack: []any{int64(5), int64(2)}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "lshiftadddivmod_code_type_addend", code: arithRawCode(t, 0xa9d000, 24), stack: []any{int64(5), cell.BeginCell().EndCell(), int64(2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "lshiftadddivmod_code_nan_addend", code: arithRawCode(t, 0xa9d000, 24), stack: []any{int64(5), vm.NaN{}, int64(2)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftadddivmod_code_wide_q_overflow", code: arithRawCode(t, 0xa9d000, 24), stack: []any{maxTVMInt, int64(1), int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftadddivmod_code_round_negative", code: arithRawCode(t, 0xa9d102, 24), stack: []any{int64(-5), int64(3), int64(2)}, exit: 0},
		{name: "lshiftadddivmod_code_ceil_negative_divisor", code: arithRawCode(t, 0xa9d202, 24), stack: []any{int64(5), int64(3), int64(-2)}, exit: 0},
		{name: "lshiftdiv_code_type_value", code: arithRawCode(t, 0xa9d400, 24), stack: []any{cell.BeginCell().EndCell(), int64(2)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "lshiftdiv_code_zero_divisor", code: arithRawCode(t, 0xa9d402, 24), stack: []any{int64(5), int64(0)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftmod_code_nan_divisor", code: arithRawCode(t, 0xa9d802, 24), stack: []any{int64(5), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftmod_code_type_divisor", code: arithRawCode(t, 0xa9d800, 24), stack: []any{int64(5), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "lshiftdivmod_code_nan_value", code: arithRawCode(t, 0xa9dc02, 24), stack: []any{vm.NaN{}, int64(2)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftdivmod_code_wide_q_overflow", code: arithRawCode(t, 0xa9dc00, 24), stack: []any{maxTVMInt, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
	}
}

func TestTVMCrossEmulatorArithOpsSignedRoundingEdges(t *testing.T) {
	tests := arithSignedRoundingEdgeCases(t)
	if len(tests) != arithSignedRoundingEdgeCaseCount {
		t.Fatalf("signed rounding edge case count = %d, want %d", len(tests), arithSignedRoundingEdgeCaseCount)
	}
	runArithParityCases(t, tests)
}

func TestTVMCrossEmulatorArithOpsSignedRoundingEdgesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := arithSignedRoundingEdgeCases(t)
	if len(tests) != arithSignedRoundingEdgeCaseCount {
		t.Fatalf("signed rounding edge case count = %d, want %d", len(tests), arithSignedRoundingEdgeCaseCount)
	}
	versions := crossEmulatorVersionAuditVersions(t, "TVM_ARITHOPS_SIGNED_ROUNDING_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runArithVersionedParityCaseWithoutExpected(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorArithOpsSignedRoundingEdgesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%arithSignedRoundingEdgeCaseCount))
	}
	for idx := 0; idx < arithSignedRoundingEdgeCaseCount; idx++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(idx))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := arithSignedRoundingEdgeCases(t)
		if len(tests) != arithSignedRoundingEdgeCaseCount {
			t.Fatalf("signed rounding edge case count = %d, want %d", len(tests), arithSignedRoundingEdgeCaseCount)
		}
		runArithVersionedParityCaseWithoutExpected(t, tests[int(rawCase)%len(tests)], version)
	})
}

func TestTVMCrossEmulatorArithOpsVersionedQuietEdges(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	type versionedArithCase struct {
		arithParityCase
		version int
	}
	tests := []versionedArithCase{
		{arithParityCase{name: "v12_qand_nan_zero_legacy", code: codeFromBuilders(t, mathop.QAND().Serialize()), stack: []any{vm.NaN{}, int64(0)}, exit: 0}, 12},
		{arithParityCase{name: "v13_qand_nan_zero_nan", code: codeFromBuilders(t, mathop.QAND().Serialize()), stack: []any{vm.NaN{}, int64(0)}, exit: 0}, 13},
		{arithParityCase{name: "v12_qor_nan_minus_one_legacy", code: codeFromBuilders(t, mathop.QOR().Serialize()), stack: []any{vm.NaN{}, int64(-1)}, exit: 0}, 12},
		{arithParityCase{name: "v13_qor_nan_minus_one_nan", code: codeFromBuilders(t, mathop.QOR().Serialize()), stack: []any{vm.NaN{}, int64(-1)}, exit: 0}, 13},
		{arithParityCase{name: "v13_qrshiftmod_bad_shift_range", code: arithRawCode(t, 0xb7a92c, 24), stack: []any{int64(35), int64(257)}, exit: int32(vmerr.CodeRangeCheck)}, 13},
		{arithParityCase{name: "v12_qlshift_bad_shift_beats_bad_value_type", code: codeFromBuilders(t, mathop.QLSHIFT().Serialize()), stack: []any{cell.BeginCell(), int64(1024)}, exit: int32(vmerr.CodeRangeCheck)}, 12},
		{arithParityCase{name: "v12_qrshift_bad_shift_range", code: codeFromBuilders(t, mathop.QRSHIFT().Serialize()), stack: []any{int64(7), int64(1024)}, exit: int32(vmerr.CodeRangeCheck)}, 12},
		{arithParityCase{name: "v13_qrshift_bad_shift_nan", code: codeFromBuilders(t, mathop.QRSHIFT().Serialize()), stack: []any{int64(7), int64(1024)}, exit: 0}, 13},
		{arithParityCase{name: "v12_qrshift_nan_shift_beats_null_value_type", code: codeFromBuilders(t, mathop.QRSHIFT().Serialize()), stack: []any{nil, vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)}, 12},
		{arithParityCase{name: "v12_qpow2_nan_exponent_range", code: codeFromBuilders(t, mathop.QPOW2().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)}, 12},
		{arithParityCase{name: "v13_qpow2_nan_exponent_nan", code: codeFromBuilders(t, mathop.QPOW2().Serialize()), stack: []any{vm.NaN{}}, exit: 0}, 13},
		{arithParityCase{name: "v12_qmulrshiftmod_bad_shift_range", code: arithRawCode(t, 0xb7a9ac, 24), stack: []any{int64(6), int64(7), int64(257)}, exit: int32(vmerr.CodeRangeCheck)}, 12},
		{arithParityCase{name: "v13_qmulrshiftmod_bad_shift_nan", code: arithRawCode(t, 0xb7a9ac, 24), stack: []any{int64(6), int64(7), int64(257)}, exit: 0}, 13},
		{arithParityCase{name: "v12_qmulrshiftmod_nan_shift_range", code: arithRawCode(t, 0xb7a9ac, 24), stack: []any{int64(6), int64(7), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)}, 12},
		{arithParityCase{name: "v13_qmulrshiftmod_nan_shift_nan", code: arithRawCode(t, 0xb7a9ac, 24), stack: []any{int64(6), int64(7), vm.NaN{}}, exit: 0}, 13},
		{arithParityCase{name: "v12_qlshiftdivmod_nan_divisor", code: arithRawCode(t, 0xb7a9cc, 24), stack: []any{int64(3), vm.NaN{}, int64(2)}, exit: 0}, 12},
		{arithParityCase{name: "v13_qlshiftdivmod_nan_divisor", code: arithRawCode(t, 0xb7a9cc, 24), stack: []any{int64(3), vm.NaN{}, int64(2)}, exit: 0}, 13},
		{arithParityCase{name: "v3_qadddivmod_min_version", code: arithRawCode(t, 0xb7a900, 24), stack: []any{int64(35), int64(4), int64(6)}, exit: int32(vmerr.CodeInvalidOpcode)}, 3},
		{arithParityCase{name: "v4_qadddivmod_min_version", code: arithRawCode(t, 0xb7a900, 24), stack: []any{int64(35), int64(4), int64(6)}, exit: 0}, 4},
		{arithParityCase{name: "v3_qaddrshiftmod_min_version", code: arithRawCode(t, 0xb7a920, 24), stack: []any{int64(35), int64(4), int64(3)}, exit: int32(vmerr.CodeInvalidOpcode)}, 3},
		{arithParityCase{name: "v4_qaddrshiftmod_min_version", code: arithRawCode(t, 0xb7a920, 24), stack: []any{int64(35), int64(4), int64(3)}, exit: 0}, 4},
		{arithParityCase{name: "v3_qmuladddivmod_min_version", code: arithRawCode(t, 0xb7a980, 24), stack: []any{int64(6), int64(7), int64(5), int64(8)}, exit: int32(vmerr.CodeInvalidOpcode)}, 3},
		{arithParityCase{name: "v4_qmuladddivmod_min_version", code: arithRawCode(t, 0xb7a980, 24), stack: []any{int64(6), int64(7), int64(5), int64(8)}, exit: 0}, 4},
		{arithParityCase{name: "v3_qmuladdrshiftmod_min_version", code: arithRawCode(t, 0xb7a9a0, 24), stack: []any{int64(6), int64(7), int64(5), int64(3)}, exit: int32(vmerr.CodeInvalidOpcode)}, 3},
		{arithParityCase{name: "v4_qmuladdrshiftmod_min_version", code: arithRawCode(t, 0xb7a9a0, 24), stack: []any{int64(6), int64(7), int64(5), int64(3)}, exit: 0}, 4},
		{arithParityCase{name: "v3_qlshiftadddivmod_min_version", code: arithRawCode(t, 0xb7a9c0, 24), stack: []any{int64(3), int64(5), int64(7), int64(2)}, exit: int32(vmerr.CodeInvalidOpcode)}, 3},
		{arithParityCase{name: "v4_qlshiftadddivmod_min_version", code: arithRawCode(t, 0xb7a9c0, 24), stack: []any{int64(3), int64(5), int64(7), int64(2)}, exit: 0}, 4},
		{arithParityCase{name: "v12_lshift_nan_52_zero", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{vm.NaN{}, int64(52)}, exit: 0}, 12},
		{arithParityCase{name: "v2_lshift_negative_int257", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{int64(-256), int64(55)}, exit: 0}, 2},
		{arithParityCase{name: "v12_lshift_nan_312_overflow", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{vm.NaN{}, int64(312)}, exit: int32(vmerr.CodeIntOverflow)}, 12},
		{arithParityCase{name: "v12_rshift_nan_12_zero", code: codeFromBuilders(t, mathop.RSHIFT().Serialize()), stack: []any{vm.NaN{}, int64(12)}, exit: 0}, 12},
		{arithParityCase{name: "v12_rshift_nan_13_minus_one", code: codeFromBuilders(t, mathop.RSHIFT().Serialize()), stack: []any{vm.NaN{}, int64(13)}, exit: 0}, 12},
		{arithParityCase{name: "v12_qrshift_nan_13_minus_one", code: codeFromBuilders(t, mathop.QRSHIFT().Serialize()), stack: []any{vm.NaN{}, int64(13)}, exit: 0}, 12},
		{arithParityCase{name: "v13_qrshift_nan_13_nan", code: codeFromBuilders(t, mathop.QRSHIFT().Serialize()), stack: []any{vm.NaN{}, int64(13)}, exit: 0}, 13},
		{arithParityCase{name: "v13_lshift_code_nan_overflow", code: codeFromBuilders(t, mathop.LSHIFTCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)}, 13},
		{arithParityCase{name: "v14_lshift_code_nan_overflow", code: codeFromBuilders(t, mathop.LSHIFTCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)}, 14},
		{arithParityCase{name: "v13_rshift_code_nan_zero", code: codeFromBuilders(t, mathop.RSHIFTCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: 0}, 13},
		{arithParityCase{name: "v14_rshift_code_nan_overflow", code: codeFromBuilders(t, mathop.RSHIFTCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow), skipReference: "bundled reference emulator predates upstream RSHIFT# v14 NaN preservation", goStack: []any{int64(0)}}, 14},
		{arithParityCase{name: "v13_qlshift_code_nan_nan", code: codeFromBuilders(t, mathop.QLSHIFTCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: 0}, 13},
		{arithParityCase{name: "v14_qlshift_code_nan_nan", code: codeFromBuilders(t, mathop.QLSHIFTCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: 0}, 14},
		{arithParityCase{name: "v13_qrshift_code_nan_zero", code: codeFromBuilders(t, mathop.QRSHIFTCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: 0}, 13},
		{arithParityCase{name: "v14_qrshift_code_nan_nan", code: codeFromBuilders(t, mathop.QRSHIFTCODE(1).Serialize()), stack: []any{vm.NaN{}}, exit: 0, skipReference: "bundled reference emulator predates upstream QRSHIFT# v14 NaN preservation", goStack: []any{vm.NaN{}}}, 14},
	}
	add := func(version int, c arithParityCase) {
		tests = append(tests, versionedArithCase{arithParityCase: c, version: version})
	}
	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_ARITHOPS_VERSION_AUDIT") {
		quietShiftExit := int32(0)
		if version < 13 {
			quietShiftExit = int32(vmerr.CodeRangeCheck)
		}
		qrShiftModExit := int32(vmerr.CodeRangeCheck)
		legacyShift52Exit := int32(0)
		if version >= 13 {
			legacyShift52Exit = int32(vmerr.CodeIntOverflow)
		}
		add(version, arithParityCase{
			name:  fmt.Sprintf("qand_nan_zero_v%d", version),
			code:  codeFromBuilders(t, mathop.QAND().Serialize()),
			stack: []any{vm.NaN{}, int64(0)},
			exit:  0,
		})
		add(version, arithParityCase{
			name:  fmt.Sprintf("qlshift_bad_shift_v%d", version),
			code:  codeFromBuilders(t, mathop.QLSHIFT().Serialize()),
			stack: []any{int64(7), int64(1024)},
			exit:  quietShiftExit,
		})
		add(version, arithParityCase{
			name:  fmt.Sprintf("qpow2_bad_exponent_v%d", version),
			code:  codeFromBuilders(t, mathop.QPOW2().Serialize()),
			stack: []any{int64(1024)},
			exit:  quietShiftExit,
		})
		add(version, arithParityCase{
			name:  fmt.Sprintf("qrshiftmod_bad_shift_v%d", version),
			code:  arithRawCode(t, 0xb7a92c, 24),
			stack: []any{int64(35), int64(257)},
			exit:  qrShiftModExit,
		})
		add(version, arithParityCase{
			name:  fmt.Sprintf("qmulrshiftmod_bad_shift_v%d", version),
			code:  arithRawCode(t, 0xb7a9ac, 24),
			stack: []any{int64(6), int64(7), int64(257)},
			exit:  quietShiftExit,
		})
		add(version, arithParityCase{
			name:  fmt.Sprintf("qlshiftdivmod_bad_shift_v%d", version),
			code:  arithRawCode(t, 0xb7a9cc, 24),
			stack: []any{int64(3), int64(5), int64(257)},
			exit:  quietShiftExit,
		})
		add(version, arithParityCase{
			name:  fmt.Sprintf("lshift_nan_52_v%d", version),
			code:  codeFromBuilders(t, mathop.LSHIFT().Serialize()),
			stack: []any{vm.NaN{}, int64(52)},
			exit:  legacyShift52Exit,
		})
		add(version, arithParityCase{
			name:  fmt.Sprintf("rshift_nan_13_v%d", version),
			code:  codeFromBuilders(t, mathop.RSHIFT().Serialize()),
			stack: []any{vm.NaN{}, int64(13)},
			exit:  legacyShift52Exit,
		})
		add(version, arithParityCase{
			name:  fmt.Sprintf("qrshift_nan_13_v%d", version),
			code:  codeFromBuilders(t, mathop.QRSHIFT().Serialize()),
			stack: []any{vm.NaN{}, int64(13)},
			exit:  0,
		})
		add(version, arithParityCase{
			name:  fmt.Sprintf("lshift_code_nan_v%d", version),
			code:  codeFromBuilders(t, mathop.LSHIFTCODE(1).Serialize()),
			stack: []any{vm.NaN{}},
			exit:  int32(vmerr.CodeIntOverflow),
		})
		rshiftCodeExit := int32(0)
		rshiftCodeSkipReference := ""
		qrshiftCodeSkipReference := ""
		if version >= 14 {
			rshiftCodeExit = int32(vmerr.CodeIntOverflow)
			rshiftCodeSkipReference = "bundled reference emulator predates upstream RSHIFT# v14 NaN preservation"
			qrshiftCodeSkipReference = "bundled reference emulator predates upstream QRSHIFT# v14 NaN preservation"
		}
		add(version, arithParityCase{
			name:          fmt.Sprintf("rshift_code_nan_v%d", version),
			code:          codeFromBuilders(t, mathop.RSHIFTCODE(1).Serialize()),
			stack:         []any{vm.NaN{}},
			exit:          rshiftCodeExit,
			skipReference: rshiftCodeSkipReference,
			goStack:       arithSkippedRShiftCodeNaNGoStack(version),
		})
		add(version, arithParityCase{
			name:  fmt.Sprintf("qlshift_code_nan_v%d", version),
			code:  codeFromBuilders(t, mathop.QLSHIFTCODE(1).Serialize()),
			stack: []any{vm.NaN{}},
			exit:  0,
		})
		add(version, arithParityCase{
			name:          fmt.Sprintf("qrshift_code_nan_v%d", version),
			code:          codeFromBuilders(t, mathop.QRSHIFTCODE(1).Serialize()),
			stack:         []any{vm.NaN{}},
			exit:          0,
			skipReference: qrshiftCodeSkipReference,
			goStack:       arithSkippedQRShiftCodeNaNGoStack(version),
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runArithVersionedParityCase(t, tt.arithParityCase, tt.version)
		})
	}
}

func FuzzTVMCrossEmulatorArithOpsVersionedQuietEdges(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		for op := uint8(0); op < 7; op++ {
			f.Add(uint8(version), op, int64(7), int16(1024), true, false, uint8(0))
			f.Add(uint8(version), op, int64(-1), int16(-1), false, false, uint8(1))
			f.Add(uint8(version), op, int64(0), int16(257), true, false, uint8(2))
			f.Add(uint8(version), op, int64(35), int16(13), true, true, uint8(3))
		}
	}
	f.Add(uint8(0), uint8(5), int64(0), int16(256), true, false, uint8(2))
	f.Add(uint8(12), uint8(6), int64(0), int16(52), true, false, uint8(2))
	f.Add(uint8(255), uint8(255), int64(-129), int16(-32768), true, true, uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawOp uint8, rawValue int64, rawShift int16, nanValue, nanShift bool, rawOther uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		runArithVersionedParityCaseWithoutExpected(t, arithVersionedQuietFuzzCase(t, rawOp, rawValue, rawShift, nanValue, nanShift, rawOther), version)
	})
}

type arithParityCase struct {
	name          string
	code          *cell.Cell
	stack         []any
	exit          int32
	skipReference string
	goStack       []any
}

func arithSkippedQRShiftCodeNaNGoStack(version int) []any {
	if version >= 14 {
		return []any{vm.NaN{}}
	}
	return nil
}

func arithSkippedRShiftCodeNaNGoStack(version int) []any {
	if version >= 14 {
		return []any{int64(0)}
	}
	return nil
}

func arithOpcodeCoverageCases(t *testing.T) []arithParityCase {
	t.Helper()

	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	minTVMInt := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))

	tests := []arithParityCase{
		{name: "pushint_tiny4_min", code: arithRawCode(t, 0x70, 8), exit: 0},
		{name: "pushint_tiny4_max", code: arithRawCode(t, 0x7f, 8), exit: 0},
		{name: "pushint_tiny8_negative", code: arithRawCode(t, 0x80fb, 16), exit: 0},
		{name: "pushint_small_positive", code: arithRawCode(t, 0x810123, 24), exit: 0},
		{name: "pushint_big_extended", code: codeFromBuilders(t, stackop.PUSHINT(new(big.Int).Lsh(big.NewInt(1), 80)).Serialize()), exit: 0},
		{name: "pushint_long_len31_invalid", code: arithRawCode(t, 0x105f, 13), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "pushpow2_first", code: arithRawCode(t, 0x8300, 16), exit: 0},
		{name: "pushpow2dec_first", code: arithRawCode(t, 0x8400, 16), exit: 0},
		{name: "pushnegpow2_first", code: arithRawCode(t, 0x8500, 16), exit: 0},

		{name: "add", code: codeFromBuilders(t, mathop.SUM().Serialize()), stack: []any{int64(5), int64(7)}, exit: 0},
		{name: "sub", code: codeFromBuilders(t, mathop.SUB().Serialize()), stack: []any{int64(5), int64(7)}, exit: 0},
		{name: "subr", code: codeFromBuilders(t, mathop.SUBR().Serialize()), stack: []any{int64(5), int64(7)}, exit: 0},
		{name: "negate", code: codeFromBuilders(t, mathop.NEGATE().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "inc", code: codeFromBuilders(t, mathop.INC().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "dec", code: codeFromBuilders(t, mathop.DEC().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "mul", code: codeFromBuilders(t, mathop.MUL().Serialize()), stack: []any{int64(6), int64(7)}, exit: 0},
		{name: "add_overflow", code: codeFromBuilders(t, mathop.SUM().Serialize()), stack: []any{maxTVMInt, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "add_type_bottom", code: codeFromBuilders(t, mathop.SUM().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(1)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "mul_overflow", code: codeFromBuilders(t, mathop.MUL().Serialize()), stack: []any{maxTVMInt, int64(2)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "sub_overflow", code: codeFromBuilders(t, mathop.SUB().Serialize()), stack: []any{minTVMInt, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "subr_overflow", code: codeFromBuilders(t, mathop.SUBR().Serialize()), stack: []any{maxTVMInt, int64(-2)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "negate_min_overflow", code: codeFromBuilders(t, mathop.NEGATE().Serialize()), stack: []any{minTVMInt}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "inc_max_overflow", code: codeFromBuilders(t, mathop.INC().Serialize()), stack: []any{maxTVMInt}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "dec_min_overflow", code: codeFromBuilders(t, mathop.DEC().Serialize()), stack: []any{minTVMInt}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "abs_min_overflow", code: codeFromBuilders(t, mathop.ABS().Serialize()), stack: []any{minTVMInt}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "abs_nan", code: codeFromBuilders(t, mathop.ABS().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "qadd", code: codeFromBuilders(t, mathop.QADD().Serialize()), stack: []any{int64(5), int64(7)}, exit: 0},
		{name: "qsub", code: codeFromBuilders(t, mathop.QSUB().Serialize()), stack: []any{int64(5), int64(7)}, exit: 0},
		{name: "qmul", code: codeFromBuilders(t, mathop.QMUL().Serialize()), stack: []any{int64(6), int64(7)}, exit: 0},

		{name: "lshift_code", code: arithRawCode(t, 0xaa02, 16), stack: []any{int64(7)}, exit: 0},
		{name: "rshift_code", code: arithRawCode(t, 0xab02, 16), stack: []any{int64(56)}, exit: 0},
		{name: "rshift_code_256", code: arithRawCode(t, 0xabff, 16), stack: []any{new(big.Int).Lsh(big.NewInt(1), 255)}, exit: 0},
		{name: "lshift", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{int64(7), int64(3)}, exit: 0},
		{name: "rshift", code: codeFromBuilders(t, mathop.RSHIFT().Serialize()), stack: []any{int64(56), int64(3)}, exit: 0},
		{name: "pow2", code: codeFromBuilders(t, mathop.POW2().Serialize()), stack: []any{int64(12)}, exit: 0},
		{name: "and", code: codeFromBuilders(t, mathop.AND().Serialize()), stack: []any{int64(0x6), int64(0x3)}, exit: 0},
		{name: "or", code: codeFromBuilders(t, mathop.OR().Serialize()), stack: []any{int64(0x6), int64(0x3)}, exit: 0},
		{name: "xor", code: codeFromBuilders(t, mathop.XOR().Serialize()), stack: []any{int64(0x6), int64(0x3)}, exit: 0},
		{name: "not", code: codeFromBuilders(t, mathop.NOT().Serialize()), stack: []any{int64(0x6)}, exit: 0},
		{name: "qlshift_code", code: arithRawCode(t, 0xb7aa02, 24), stack: []any{int64(7)}, exit: 0},
		{name: "qrshift_code", code: arithRawCode(t, 0xb7ab02, 24), stack: []any{int64(56)}, exit: 0},
		{name: "qlshift_code_256_overflow_nan", code: arithRawCode(t, 0xb7aaff, 24), stack: []any{int64(1)}, exit: 0},
		{name: "qrshift_code_256_positive_zero", code: arithRawCode(t, 0xb7abff, 24), stack: []any{int64(1)}, exit: 0},
		{name: "qrshift_code_256_negative_minus_one", code: arithRawCode(t, 0xb7abff, 24), stack: []any{int64(-1)}, exit: 0},
		{name: "qlshift", code: codeFromBuilders(t, mathop.QLSHIFT().Serialize()), stack: []any{int64(7), int64(3)}, exit: 0},
		{name: "qrshift", code: codeFromBuilders(t, mathop.QRSHIFT().Serialize()), stack: []any{int64(56), int64(3)}, exit: 0},
		{name: "qpow2", code: codeFromBuilders(t, mathop.QPOW2().Serialize()), stack: []any{int64(12)}, exit: 0},
		{name: "qand", code: codeFromBuilders(t, mathop.QAND().Serialize()), stack: []any{int64(0x6), int64(0x3)}, exit: 0},
		{name: "qor", code: codeFromBuilders(t, mathop.QOR().Serialize()), stack: []any{int64(0x6), int64(0x3)}, exit: 0},
		{name: "qxor", code: codeFromBuilders(t, mathop.QXOR().Serialize()), stack: []any{int64(0x6), int64(0x3)}, exit: 0},
		{name: "qnot", code: codeFromBuilders(t, mathop.QNOT().Serialize()), stack: []any{int64(0x6)}, exit: 0},
		{name: "addrshift_code_mod_positive", code: codeFromBuilders(t, mathop.ADDRSHIFTCODEMOD(3).Serialize()), stack: []any{int64(9), int64(4)}, exit: 0},
		{name: "addrshiftr_code_mod_negative", code: codeFromBuilders(t, mathop.ADDRSHIFTRCODEMOD(3).Serialize()), stack: []any{int64(-9), int64(4)}, exit: 0},
		{name: "addrshiftc_code_mod_negative", code: codeFromBuilders(t, mathop.ADDRSHIFTCCODEMOD(3).Serialize()), stack: []any{int64(-9), int64(4)}, exit: 0},
		{name: "muladdrshift_code_mod_positive", code: codeFromBuilders(t, mathop.MULADDRSHIFTCODEMOD(3).Serialize()), stack: []any{int64(9), int64(2), int64(4)}, exit: 0},
		{name: "muladdrshiftr_code_mod_negative", code: codeFromBuilders(t, mathop.MULADDRSHIFTRCODEMOD(3).Serialize()), stack: []any{int64(-11), int64(2), int64(4)}, exit: 0},
		{name: "muladdrshiftc_code_mod_negative", code: codeFromBuilders(t, mathop.MULADDRSHIFTCCODEMOD(3).Serialize()), stack: []any{int64(-11), int64(2), int64(4)}, exit: 0},
		{name: "addrshift_code_mod_nan_overflow", code: codeFromBuilders(t, mathop.ADDRSHIFTCODEMOD(3).Serialize()), stack: []any{vm.NaN{}, int64(4)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muladdrshift_code_mod_nan_overflow", code: codeFromBuilders(t, mathop.MULADDRSHIFTCODEMOD(3).Serialize()), stack: []any{int64(9), vm.NaN{}, int64(4)}, exit: int32(vmerr.CodeIntOverflow)},

		{name: "fits", code: codeFromBuilders(t, mathop.FITS(7).Serialize()), stack: []any{int64(63)}, exit: 0},
		{name: "ufits", code: codeFromBuilders(t, mathop.UFITS(7).Serialize()), stack: []any{int64(127)}, exit: 0},
		{name: "fitsx", code: codeFromBuilders(t, mathop.FITSX().Serialize()), stack: []any{int64(63), int64(7)}, exit: 0},
		{name: "ufitsx", code: codeFromBuilders(t, mathop.UFITSX().Serialize()), stack: []any{int64(127), int64(7)}, exit: 0},
		{name: "bitsize_positive", code: codeFromBuilders(t, mathop.BITSIZE().Serialize()), stack: []any{int64(127)}, exit: 0},
		{name: "ubitsize_positive", code: codeFromBuilders(t, mathop.UBITSIZE().Serialize()), stack: []any{int64(127)}, exit: 0},
		{name: "qfits", code: codeFromBuilders(t, mathop.QFITS(7).Serialize()), stack: []any{int64(63)}, exit: 0},
		{name: "qufits", code: codeFromBuilders(t, mathop.QUFITS(7).Serialize()), stack: []any{int64(127)}, exit: 0},
		{name: "qfitsx", code: codeFromBuilders(t, mathop.QFITSX().Serialize()), stack: []any{int64(63), int64(7)}, exit: 0},
		{name: "qufitsx", code: codeFromBuilders(t, mathop.QUFITSX().Serialize()), stack: []any{int64(127), int64(7)}, exit: 0},
		{name: "qbitsize", code: codeFromBuilders(t, mathop.QBITSIZE().Serialize()), stack: []any{int64(-3)}, exit: 0},
		{name: "qubitsize", code: codeFromBuilders(t, mathop.QUBITSIZE().Serialize()), stack: []any{int64(127)}, exit: 0},

		{name: "min", code: codeFromBuilders(t, mathop.MIN().Serialize()), stack: []any{int64(5), int64(2)}, exit: 0},
		{name: "max", code: codeFromBuilders(t, mathop.MAX().Serialize()), stack: []any{int64(5), int64(2)}, exit: 0},
		{name: "min_underflow", code: codeFromBuilders(t, mathop.MIN().Serialize()), exit: int32(vmerr.CodeStackUnderflow)},
		{name: "max_nan_bottom", code: codeFromBuilders(t, mathop.MAX().Serialize()), stack: []any{vm.NaN{}, int64(7)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "min_type_bottom", code: codeFromBuilders(t, mathop.MIN().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(7)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "abs", code: codeFromBuilders(t, mathop.ABS().Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "qmin", code: codeFromBuilders(t, mathop.QMIN().Serialize()), stack: []any{int64(5), int64(2)}, exit: 0},
		{name: "qmax", code: codeFromBuilders(t, mathop.QMAX().Serialize()), stack: []any{int64(5), int64(2)}, exit: 0},
		{name: "qabs", code: codeFromBuilders(t, mathop.QABS().Serialize()), stack: []any{int64(-7)}, exit: 0},

		{name: "sgn_zero", code: codeFromBuilders(t, mathop.SGN().Serialize()), stack: []any{int64(0)}, exit: 0},
		{name: "equal", code: codeFromBuilders(t, mathop.EQUAL().Serialize()), stack: []any{int64(7), int64(7)}, exit: 0},
		{name: "equal_type_top", code: codeFromBuilders(t, mathop.EQUAL().Serialize()), stack: []any{int64(7), cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "leq", code: codeFromBuilders(t, mathop.LEQ().Serialize()), stack: []any{int64(7), int64(7)}, exit: 0},
		{name: "less_nan_top", code: codeFromBuilders(t, mathop.LESS().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "greater", code: codeFromBuilders(t, mathop.GREATER().Serialize()), stack: []any{int64(9), int64(7)}, exit: 0},
		{name: "neq", code: codeFromBuilders(t, mathop.NEQ().Serialize()), stack: []any{int64(9), int64(7)}, exit: 0},
		{name: "and_type_bottom", code: codeFromBuilders(t, mathop.AND().Serialize()), stack: []any{cell.BeginCell().EndCell(), int64(7)}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "xor_nan_bottom", code: codeFromBuilders(t, mathop.XOR().Serialize()), stack: []any{vm.NaN{}, int64(7)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "not_nan", code: codeFromBuilders(t, mathop.NOT().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "gtint_negative", code: codeFromBuilders(t, mathop.GTINT(-5).Serialize()), stack: []any{int64(-4)}, exit: 0},
		{name: "eqint_nan", code: codeFromBuilders(t, mathop.EQINT(7).Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lessint_underflow", code: codeFromBuilders(t, mathop.LESSINT(7).Serialize()), exit: int32(vmerr.CodeStackUnderflow)},
		{name: "neqint_type", code: codeFromBuilders(t, mathop.NEQINT(7).Serialize()), stack: []any{cell.BeginCell().EndCell()}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "isnan_false", code: codeFromBuilders(t, mathop.ISNAN().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "chknan_finite", code: codeFromBuilders(t, mathop.CHKNAN().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "qsgn", code: codeFromBuilders(t, mathop.QSGN().Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "qless", code: codeFromBuilders(t, mathop.QLESS().Serialize()), stack: []any{int64(1), int64(2)}, exit: 0},
		{name: "qequal", code: codeFromBuilders(t, mathop.QEQUAL().Serialize()), stack: []any{int64(7), int64(7)}, exit: 0},
		{name: "qleq", code: codeFromBuilders(t, mathop.QLEQ().Serialize()), stack: []any{int64(7), int64(7)}, exit: 0},
		{name: "qgreater", code: codeFromBuilders(t, mathop.QGREATER().Serialize()), stack: []any{int64(9), int64(7)}, exit: 0},
		{name: "qneq", code: codeFromBuilders(t, mathop.QNEQ().Serialize()), stack: []any{int64(9), int64(7)}, exit: 0},
		{name: "qgeq", code: codeFromBuilders(t, mathop.QGEQ().Serialize()), stack: []any{int64(9), int64(7)}, exit: 0},
		{name: "qcmp", code: codeFromBuilders(t, mathop.QCMP().Serialize()), stack: []any{int64(9), int64(7)}, exit: 0},
		{name: "qeqint", code: codeFromBuilders(t, mathop.QEQINT(-5).Serialize()), stack: []any{int64(-5)}, exit: 0},
		{name: "qlessint", code: codeFromBuilders(t, mathop.QLESSINT(-5).Serialize()), stack: []any{int64(-6)}, exit: 0},
		{name: "qneqint", code: codeFromBuilders(t, mathop.QNEQINT(-5).Serialize()), stack: []any{int64(-4)}, exit: 0},
	}

	tests = append(tests, arithDivModFamilyCases(t, "divmod", 0xa90, 12, false)...)
	tests = append(tests, arithDivModFamilyCases(t, "qdivmod", 0xb7a90, 20, true)...)
	tests = append(tests, arithShiftModFamilyCases(t, "rshiftmod", 0xa92, 12, false)...)
	tests = append(tests, arithShiftModFamilyCases(t, "qrshiftmod", 0xb7a92, 20, true)...)
	tests = append(tests, arithImmediateShiftModFamilyCases(t, "rshiftmod_code", 0xa93)...)
	tests = append(tests, arithMulDivModFamilyCases(t, "muldivmod", 0xa98, 12, false)...)
	tests = append(tests, arithMulDivModFamilyCases(t, "qmuldivmod", 0xb7a98, 20, true)...)
	tests = append(tests, arithMulShiftModFamilyCases(t, "mulrshiftmod", 0xa9a, 12, false)...)
	tests = append(tests, arithMulShiftModFamilyCases(t, "qmulrshiftmod", 0xb7a9a, 20, true)...)
	tests = append(tests, arithImmediateMulShiftModFamilyCases(t, "mulrshiftmod_code", 0xa9b)...)
	tests = append(tests, arithShiftDivModFamilyCases(t, "lshiftdivmod", 0xa9c, 12, false)...)
	tests = append(tests, arithShiftDivModFamilyCases(t, "qlshiftdivmod", 0xb7a9c, 20, true)...)
	tests = append(tests, arithImmediateShiftDivModFamilyCases(t, "lshiftdivmod_code", 0xa9d)...)
	tests = append(tests,
		arithParityCase{name: "divmod_invalid_round", code: arithRawCode(t, 0xa903, 16), exit: int32(vmerr.CodeInvalidOpcode)},
		arithParityCase{name: "rshiftmod_invalid_round", code: arithRawCode(t, 0xa927, 16), exit: int32(vmerr.CodeInvalidOpcode)},
		arithParityCase{name: "rshiftmod_code_invalid_round", code: arithRawCode(t, 0xa93300, 24), exit: int32(vmerr.CodeInvalidOpcode)},
		arithParityCase{name: "muldivmod_invalid_round", code: arithRawCode(t, 0xa987, 16), exit: int32(vmerr.CodeInvalidOpcode)},
		arithParityCase{name: "mulrshiftmod_invalid_round", code: arithRawCode(t, 0xa9a7, 16), exit: int32(vmerr.CodeInvalidOpcode)},
		arithParityCase{name: "mulrshiftmod_code_invalid_round", code: arithRawCode(t, 0xa9b300, 24), exit: int32(vmerr.CodeInvalidOpcode)},
		arithParityCase{name: "lshiftdivmod_invalid_round", code: arithRawCode(t, 0xa9c7, 16), exit: int32(vmerr.CodeInvalidOpcode)},
		arithParityCase{name: "lshiftdivmod_code_invalid_round", code: arithRawCode(t, 0xa9d300, 24), exit: int32(vmerr.CodeInvalidOpcode)},
	)

	return tests
}

func TestTVMCrossEmulatorArithOpsOpcodeCoverage(t *testing.T) {
	runArithParityCases(t, arithOpcodeCoverageCases(t))
}

func TestTVMCrossEmulatorArithOpsOpcodeCoverageAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := arithOpcodeCoverageCases(t)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_ARITHOPS_OPCODE_COVERAGE_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runArithVersionedParityCaseWithoutExpected(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorArithOpsOpcodeCoverageGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		for _, rawCase := range []uint16{0, 1, 2, 7, 31, 63, 127, 255, 511} {
			f.Add(uint8(version), rawCase)
		}
	}
	f.Add(uint8(255), uint16(65535))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := arithOpcodeCoverageCases(t)
		runArithVersionedParityCaseWithoutExpected(t, tests[int(rawCase)%len(tests)], version)
	})
}

func runArithParityCases(t *testing.T, tests []arithParityCase) {
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
			if tt.skipReference != "" {
				if goRes.exitCode != tt.exit {
					t.Fatalf("unexpected go exit code: got=%d expected=%d", goRes.exitCode, tt.exit)
				}
				assertArithSkippedGoStack(t, goRes.stack, tt.goStack)
				t.Skip(tt.skipReference)
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

func runArithVersionedParityCase(t *testing.T, tt arithParityCase, globalVersion int) {
	t.Helper()
	runArithVersionedParityCaseWithExpected(t, tt, globalVersion, &tt.exit)
}

func runArithVersionedParityCaseWithoutExpected(t *testing.T, tt arithParityCase, globalVersion int) {
	t.Helper()
	runArithVersionedParityCaseWithExpected(t, tt, globalVersion, nil)
}

func runArithVersionedParityCaseWithExpected(t *testing.T, tt arithParityCase, globalVersion int, expectedExit *int32) {
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
	if tt.skipReference != "" {
		if expectedExit != nil && goRes.exitCode != *expectedExit {
			t.Fatalf("unexpected go exit code: got=%d expected=%d", goRes.exitCode, *expectedExit)
		}
		assertArithSkippedGoStack(t, goRes.stack, tt.goStack)
		t.Skip(tt.skipReference)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if expectedExit != nil && (goRes.exitCode != *expectedExit || refRes.exitCode != *expectedExit) {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, *expectedExit)
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

func assertArithSkippedGoStack(t *testing.T, got *cell.Cell, want []any) {
	t.Helper()

	if want == nil {
		return
	}
	wantStack, err := buildCrossStack(want...)
	if err != nil {
		t.Fatalf("failed to build expected go stack: %v", err)
	}
	wantStackCell, err := stackToCell(wantStack)
	if err != nil {
		t.Fatalf("failed to serialize expected go stack: %v", err)
	}
	gotStackCell, err := normalizeStackCell(got)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	wantStackCell, err = normalizeStackCell(wantStackCell)
	if err != nil {
		t.Fatalf("failed to normalize expected go stack: %v", err)
	}
	if !bytes.Equal(gotStackCell.Hash(), wantStackCell.Hash()) {
		t.Fatalf("go stack mismatch:\ngo=%s\nwant=%s", gotStackCell.Dump(), wantStackCell.Dump())
	}
}

func arithVersionedQuietFuzzCase(t *testing.T, rawOp uint8, rawValue int64, rawShift int16, nanValue, nanShift bool, rawOther uint8) arithParityCase {
	t.Helper()

	value := int64(rawValue%257) - 128
	other := arithVersionedQuietSpecialValue(rawValue, rawOther)
	shift := any(int64(rawShift))
	if nanShift {
		shift = vm.NaN{}
	}
	maybeNaN := func(v int64, nan bool) any {
		if nan {
			return vm.NaN{}
		}
		return v
	}

	switch rawOp % 7 {
	case 0:
		x := maybeNaN(value, nanValue)
		y := maybeNaN(other, nanShift)
		if !nanValue && !nanShift {
			x = vm.NaN{}
		}
		return arithParityCase{
			name:  "fuzz_qand_versioned_quiet",
			code:  codeFromBuilders(t, mathop.QAND().Serialize()),
			stack: []any{x, y},
		}
	case 1:
		x := maybeNaN(value, nanValue)
		y := maybeNaN(other, nanShift)
		if !nanValue && !nanShift {
			x = vm.NaN{}
		}
		return arithParityCase{
			name:  "fuzz_qor_versioned_quiet",
			code:  codeFromBuilders(t, mathop.QOR().Serialize()),
			stack: []any{x, y},
		}
	case 2:
		return arithParityCase{
			name:  "fuzz_qlshift_versioned_quiet",
			code:  codeFromBuilders(t, mathop.QLSHIFT().Serialize()),
			stack: []any{maybeNaN(value, nanValue), shift},
		}
	case 3:
		return arithParityCase{
			name:  "fuzz_qrshift_versioned_quiet",
			code:  codeFromBuilders(t, mathop.QRSHIFT().Serialize()),
			stack: []any{maybeNaN(value, nanValue), shift},
		}
	case 4:
		return arithParityCase{
			name:  "fuzz_qpow2_versioned_quiet",
			code:  codeFromBuilders(t, mathop.QPOW2().Serialize()),
			stack: []any{shift},
		}
	case 5:
		return arithParityCase{
			name:  "fuzz_qmulrshiftmod_versioned_quiet",
			code:  arithRawCode(t, 0xb7a9ac, 24),
			stack: []any{maybeNaN(value, nanValue), int64(rawOther%17) + 2, shift},
		}
	default:
		return arithParityCase{
			name:  "fuzz_qlshiftdivmod_versioned_quiet",
			code:  arithRawCode(t, 0xb7a9cc, 24),
			stack: []any{maybeNaN(value, nanValue), int64(rawOther%17) + 1, shift},
		}
	}
}

func arithVersionedQuietSpecialValue(rawValue int64, rawOther uint8) int64 {
	switch rawOther % 4 {
	case 0:
		return 0
	case 1:
		return -1
	case 2:
		return 1
	default:
		return int64(rawValue%31) - 15
	}
}

func arithRawCode(t *testing.T, value uint64, bits uint) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t, cell.BeginCell().MustStoreUInt(value, bits))
}

func arithDivModFamilyCases(t *testing.T, name string, prefix uint64, prefixBits uint, quiet bool) []arithParityCase {
	t.Helper()

	return arithCompoundCases(t, name, prefix, prefixBits, quiet, func(d int) []any {
		if d == 0 {
			return []any{int64(35), int64(4), int64(6)}
		}
		return []any{int64(35), int64(6)}
	})
}

func arithShiftModFamilyCases(t *testing.T, name string, prefix uint64, prefixBits uint, quiet bool) []arithParityCase {
	t.Helper()

	return arithCompoundCases(t, name, prefix, prefixBits, quiet, func(d int) []any {
		if d == 0 {
			return []any{int64(35), int64(4), int64(3)}
		}
		return []any{int64(35), int64(3)}
	})
}

func arithImmediateShiftModFamilyCases(t *testing.T, name string, prefix uint64) []arithParityCase {
	t.Helper()

	return arithImmediateCompoundCases(t, name, prefix, func(d int) []any {
		if d == 0 {
			return []any{int64(35), int64(4)}
		}
		return []any{int64(35)}
	})
}

func arithMulDivModFamilyCases(t *testing.T, name string, prefix uint64, prefixBits uint, quiet bool) []arithParityCase {
	t.Helper()

	return arithCompoundCases(t, name, prefix, prefixBits, quiet, func(d int) []any {
		if d == 0 {
			return []any{int64(6), int64(7), int64(5), int64(8)}
		}
		return []any{int64(6), int64(7), int64(8)}
	})
}

func arithMulShiftModFamilyCases(t *testing.T, name string, prefix uint64, prefixBits uint, quiet bool) []arithParityCase {
	t.Helper()

	return arithCompoundCases(t, name, prefix, prefixBits, quiet, func(d int) []any {
		if d == 0 {
			return []any{int64(6), int64(7), int64(5), int64(3)}
		}
		return []any{int64(6), int64(7), int64(3)}
	})
}

func arithImmediateMulShiftModFamilyCases(t *testing.T, name string, prefix uint64) []arithParityCase {
	t.Helper()

	return arithImmediateCompoundCases(t, name, prefix, func(d int) []any {
		if d == 0 {
			return []any{int64(6), int64(7), int64(5)}
		}
		return []any{int64(6), int64(7)}
	})
}

func arithShiftDivModFamilyCases(t *testing.T, name string, prefix uint64, prefixBits uint, quiet bool) []arithParityCase {
	t.Helper()

	return arithCompoundCases(t, name, prefix, prefixBits, quiet, func(d int) []any {
		if d == 0 {
			return []any{int64(3), int64(5), int64(7), int64(2)}
		}
		return []any{int64(3), int64(7), int64(2)}
	})
}

func arithImmediateShiftDivModFamilyCases(t *testing.T, name string, prefix uint64) []arithParityCase {
	t.Helper()

	return arithImmediateCompoundCases(t, name, prefix, func(d int) []any {
		if d == 0 {
			return []any{int64(3), int64(5), int64(7)}
		}
		return []any{int64(3), int64(7)}
	})
}

func arithCompoundCases(t *testing.T, name string, prefix uint64, prefixBits uint, quiet bool, stack func(int) []any) []arithParityCase {
	t.Helper()

	var cases []arithParityCase
	for d := 0; d <= 3; d++ {
		for round := 0; round <= 2; round++ {
			suffix := arithCompoundSuffix(d, round)
			cases = append(cases, arithParityCase{
				name:  arithCompoundName(name, d, round, quiet),
				code:  arithRawCode(t, (prefix<<4)|suffix, prefixBits+4),
				stack: stack(d),
				exit:  0,
			})
		}
	}
	return cases
}

func arithImmediateCompoundCases(t *testing.T, name string, prefix uint64, stack func(int) []any) []arithParityCase {
	t.Helper()

	const shift = 3

	var cases []arithParityCase
	for d := 0; d <= 3; d++ {
		for round := 0; round <= 2; round++ {
			suffix := arithCompoundSuffix(d, round)
			cases = append(cases, arithParityCase{
				name:  arithCompoundName(name, d, round, false),
				code:  arithRawCode(t, (prefix<<12)|(suffix<<8)|(shift-1), 24),
				stack: stack(d),
				exit:  0,
			})
		}
	}
	return cases
}

func arithCompoundSuffix(d, round int) uint64 {
	return uint64((d << 2) | round)
}

func arithCompoundName(name string, d, round int, quiet bool) string {
	roundSuffix := [...]string{"", "r", "c"}[round]
	return name + "_d" + string(rune('0'+d)) + roundSuffix
}
