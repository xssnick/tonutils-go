//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
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
		{name: "add_short_stack_nan", code: codeFromBuilders(t, mathop.SUM().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "qadd_overflow", code: codeFromBuilders(t, mathop.QADD().Serialize()), stack: []any{maxTVMInt, int64(1)}, exit: 0},
		{name: "qadd_short_stack_null", code: codeFromBuilders(t, mathop.QADD().Serialize()), stack: []any{nil}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "qsub_nan", code: codeFromBuilders(t, mathop.QSUB().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qmul_nan", code: codeFromBuilders(t, mathop.QMUL().Serialize()), stack: []any{vm.NaN{}, int64(2)}, exit: 0},
		{name: "qsubr", code: codeFromBuilders(t, mathop.QSUBR().Serialize()), stack: []any{int64(2), int64(5)}, exit: 0},
		{name: "qnegate", code: codeFromBuilders(t, mathop.QNEGATE().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "qinc", code: codeFromBuilders(t, mathop.QINC().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "qdec", code: codeFromBuilders(t, mathop.QDEC().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "qaddint_negative", code: codeFromBuilders(t, mathop.QADDINT(-5).Serialize()), stack: []any{int64(9)}, exit: 0},
		{name: "qmulint_negative", code: codeFromBuilders(t, mathop.QMULINT(-3).Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "qnot_nan", code: codeFromBuilders(t, mathop.QNOT().Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qand_nan", code: codeFromBuilders(t, mathop.QAND().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qor_nan", code: codeFromBuilders(t, mathop.QOR().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qxor_nan", code: codeFromBuilders(t, mathop.QXOR().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qlshift_nan", code: codeFromBuilders(t, mathop.QLSHIFT().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qrshift_nan", code: codeFromBuilders(t, mathop.QRSHIFT().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "lshift_nan_count_range", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "lshift_short_stack_nan_count", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "pow2_nan_count_range", code: codeFromBuilders(t, mathop.POW2().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "qpow2_overflow", code: codeFromBuilders(t, mathop.QPOW2().Serialize()), stack: []any{int64(300)}, exit: 0},
		{name: "qdiv", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A904, 24)), stack: []any{int64(10), int64(3)}, exit: 0},
		{name: "qdiv_zero_nan", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A904, 24)), stack: []any{int64(10), int64(0)}, exit: 0},
		{name: "qdivmod", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A90C, 24)), stack: []any{int64(10), int64(3)}, exit: 0},
		{name: "adddivmod_short_stack_nan_divisor", code: codeFromBuilders(t, mathop.ADDDIVMOD().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "qaddrshiftmod", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A920, 24)), stack: []any{int64(5), int64(3), int64(1)}, exit: 0},
		{name: "qmuldivmod", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A98C, 24)), stack: []any{int64(6), int64(7), int64(5)}, exit: 0},
		{name: "qmuldivmod_short_stack_nan_divisor", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A98C, 24)), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "qmulrshiftmod", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A9AC, 24)), stack: []any{int64(6), int64(7), int64(3)}, exit: 0},
		{name: "qmulrshiftmod_bad_shift_nan", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A9AC, 24)), stack: []any{int64(6), int64(7), int64(300)}, exit: 0},
		{name: "qlshiftdivmod", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A9CC, 24)), stack: []any{int64(3), int64(5), int64(2)}, exit: 0},
		{name: "mulmod_negative_divisor", code: codeFromBuilders(t, mathop.MULMOD().Serialize()), stack: []any{int64(5), int64(1), int64(-3)}, exit: 0},
		{name: "qmulmod_negative_divisor", code: codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xB7A988, 24)), stack: []any{int64(5), int64(1), int64(-3)}, exit: 0},
		{name: "min_nan_top_direct", code: codeFromBuilders(t, mathop.MIN().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "muldiv_nan_top_direct", code: codeFromBuilders(t, mathop.MULDIV().Serialize()), stack: []any{int64(2), int64(3), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "adddivmod_nan_middle_direct", code: codeFromBuilders(t, mathop.ADDDIVMOD().Serialize()), stack: []any{int64(5), vm.NaN{}, int64(2)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "lshiftdiv_nan_divisor_direct", code: codeFromBuilders(t, mathop.LSHIFTDIV().Serialize()), stack: []any{int64(5), vm.NaN{}, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "fits_fail", code: codeFromBuilders(t, mathop.FITS(6).Serialize()), stack: []any{int64(64)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "ufits_fail", code: codeFromBuilders(t, mathop.UFITS(6).Serialize()), stack: []any{int64(-1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "bitsize_negative", code: codeFromBuilders(t, mathop.BITSIZE().Serialize()), stack: []any{int64(-3)}, exit: 0},
		{name: "ubitsize_negative", code: codeFromBuilders(t, mathop.UBITSIZE().Serialize()), stack: []any{int64(-1)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "qfits_fail", code: codeFromBuilders(t, mathop.QFITS(7).Serialize()), stack: []any{int64(128)}, exit: 0},
		{name: "fitsx_fail", code: codeFromBuilders(t, mathop.FITSX().Serialize()), stack: []any{int64(128), int64(7)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "fitsx_nan_bits_range", code: codeFromBuilders(t, mathop.FITSX().Serialize()), stack: []any{int64(128), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "fitsx_short_stack_nan_bits", code: codeFromBuilders(t, mathop.FITSX().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeStackUnderflow)},
		{name: "qfitsx_fail", code: codeFromBuilders(t, mathop.QFITSX().Serialize()), stack: []any{int64(128), int64(8)}, exit: 0},
		{name: "qfitsx_bad_width_range", code: codeFromBuilders(t, mathop.QFITSX().Serialize()), stack: []any{int64(7), int64(1024)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "qfitsx_nan_width_range", code: codeFromBuilders(t, mathop.QFITSX().Serialize()), stack: []any{int64(7), vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "qufits_fail", code: codeFromBuilders(t, mathop.QUFITS(7).Serialize()), stack: []any{int64(-1)}, exit: 0},
		{name: "qufitsx_fail", code: codeFromBuilders(t, mathop.QUFITSX().Serialize()), stack: []any{int64(-1), int64(8)}, exit: 0},
		{name: "qufitsx_bad_width_range", code: codeFromBuilders(t, mathop.QUFITSX().Serialize()), stack: []any{int64(7), int64(1024)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "qbitsize_nan", code: codeFromBuilders(t, mathop.QBITSIZE().Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qubitsize_negative", code: codeFromBuilders(t, mathop.QUBITSIZE().Serialize()), stack: []any{int64(-1)}, exit: 0},
		{name: "minmax", code: codeFromBuilders(t, mathop.MINMAX().Serialize()), stack: []any{int64(5), int64(2)}, exit: 0},
		{name: "qminmax_nan", code: codeFromBuilders(t, mathop.QMINMAX().Serialize()), stack: []any{vm.NaN{}, int64(5)}, exit: 0},
		{name: "qabs_negative", code: codeFromBuilders(t, mathop.QABS().Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "less_order", code: codeFromBuilders(t, mathop.LESS().Serialize()), stack: []any{int64(1), int64(2)}, exit: 0},
		{name: "geq_order", code: codeFromBuilders(t, mathop.GEQ().Serialize()), stack: []any{int64(3), int64(2)}, exit: 0},
		{name: "eqint_negative", code: codeFromBuilders(t, mathop.EQINT(-5).Serialize()), stack: []any{int64(-5)}, exit: 0},
		{name: "lessint_negative", code: codeFromBuilders(t, mathop.LESSINT(-5).Serialize()), stack: []any{int64(-6)}, exit: 0},
		{name: "neqint_negative", code: codeFromBuilders(t, mathop.NEQINT(-5).Serialize()), stack: []any{int64(-4)}, exit: 0},
		{name: "sgn_negative", code: codeFromBuilders(t, mathop.SGN().Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "cmp_less", code: codeFromBuilders(t, mathop.CMP().Serialize()), stack: []any{int64(2), int64(5)}, exit: 0},
		{name: "isnan_true", code: codeFromBuilders(t, mathop.ISNAN().Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "chknan", code: codeFromBuilders(t, mathop.CHKNAN().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "qsgn_nan", code: codeFromBuilders(t, mathop.QSGN().Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qcmp_nan", code: codeFromBuilders(t, mathop.QCMP().Serialize()), stack: []any{vm.NaN{}, int64(1)}, exit: 0},
		{name: "qeqint_nan", code: codeFromBuilders(t, mathop.QEQINT(7).Serialize()), stack: []any{vm.NaN{}}, exit: 0},
		{name: "qgtint_negative", code: codeFromBuilders(t, mathop.QGTINT(-5).Serialize()), stack: []any{int64(-4)}, exit: 0},
		{name: "iszero_true", code: codeFromBuilders(t, mathop.ISZERO().Serialize()), stack: []any{int64(0)}, exit: 0},
		{name: "ispos_true", code: codeFromBuilders(t, mathop.ISPOS().Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "isneg_true", code: codeFromBuilders(t, mathop.ISNEG().Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "isnneg_zero", code: codeFromBuilders(t, mathop.ISNNEG().Serialize()), stack: []any{int64(0)}, exit: 0},
		{name: "isnpos_zero", code: codeFromBuilders(t, mathop.ISNPOS().Serialize()), stack: []any{int64(0)}, exit: 0},
		{name: "rshiftfloor_negative", code: codeFromBuilders(t, mathop.RSHIFTFLOOR().Serialize()), stack: []any{int64(-7), int64(1)}, exit: 0},
		{name: "rshiftcodefloor_negative", code: codeFromBuilders(t, mathop.RSHIFTCODEFLOOR(1).Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "rshiftc_256_boundary", code: codeFromBuilders(t, mathop.RSHIFTC().Serialize()), stack: []any{int64(7), int64(256)}, exit: 0},
		{name: "rshiftc_257_range", code: codeFromBuilders(t, mathop.RSHIFTC().Serialize()), stack: []any{int64(7), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "modpow2_257_range", code: codeFromBuilders(t, mathop.MODPOW2().Serialize()), stack: []any{int64(7), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "rshiftmod_257_range", code: codeFromBuilders(t, mathop.RSHIFTMOD().Serialize()), stack: []any{int64(7), int64(257)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "divmodc_quotient_overflow", code: codeFromBuilders(t, mathop.DIVMODC().Serialize()), stack: []any{minTVMInt, int64(-1)}, exit: int32(vmerr.CodeIntOverflow)},
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

func TestTVMCrossEmulatorArithOpsSignedRoundingEdges(t *testing.T) {
	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	largePositive := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(3))
	largeNegative := new(big.Int).Neg(new(big.Int).Set(largePositive))

	tests := []arithParityCase{
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
		{name: "lshift_max_overflow", code: codeFromBuilders(t, mathop.LSHIFT().Serialize()), stack: []any{maxTVMInt, int64(1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "qlshift_max_overflow_nan", code: codeFromBuilders(t, mathop.QLSHIFT().Serialize()), stack: []any{maxTVMInt, int64(1)}, exit: 0},
		{name: "qlshiftdivmod_bad_shift_nan", code: arithRawCode(t, 0xb7a9cc, 24), stack: []any{int64(3), int64(5), int64(257)}, exit: 0},
		{name: "q_rshiftmod_code_unregistered", code: arithRawCode(t, 0xb7a93c02, 32), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "q_mulrshiftmod_code_unregistered", code: arithRawCode(t, 0xb7a9bc02, 32), exit: int32(vmerr.CodeInvalidOpcode)},
		{name: "q_lshiftdivmod_code_unregistered", code: arithRawCode(t, 0xb7a9dc02, 32), exit: int32(vmerr.CodeInvalidOpcode)},
	}

	runArithParityCases(t, tests)
}

type arithParityCase struct {
	name  string
	code  *cell.Cell
	stack []any
	exit  int32
}

func TestTVMCrossEmulatorArithOpsOpcodeCoverage(t *testing.T) {
	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

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
		{name: "qlshift", code: codeFromBuilders(t, mathop.QLSHIFT().Serialize()), stack: []any{int64(7), int64(3)}, exit: 0},
		{name: "qrshift", code: codeFromBuilders(t, mathop.QRSHIFT().Serialize()), stack: []any{int64(56), int64(3)}, exit: 0},
		{name: "qpow2", code: codeFromBuilders(t, mathop.QPOW2().Serialize()), stack: []any{int64(12)}, exit: 0},
		{name: "qand", code: codeFromBuilders(t, mathop.QAND().Serialize()), stack: []any{int64(0x6), int64(0x3)}, exit: 0},
		{name: "qor", code: codeFromBuilders(t, mathop.QOR().Serialize()), stack: []any{int64(0x6), int64(0x3)}, exit: 0},
		{name: "qxor", code: codeFromBuilders(t, mathop.QXOR().Serialize()), stack: []any{int64(0x6), int64(0x3)}, exit: 0},
		{name: "qnot", code: codeFromBuilders(t, mathop.QNOT().Serialize()), stack: []any{int64(0x6)}, exit: 0},

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
		{name: "abs", code: codeFromBuilders(t, mathop.ABS().Serialize()), stack: []any{int64(-7)}, exit: 0},
		{name: "qmin", code: codeFromBuilders(t, mathop.QMIN().Serialize()), stack: []any{int64(5), int64(2)}, exit: 0},
		{name: "qmax", code: codeFromBuilders(t, mathop.QMAX().Serialize()), stack: []any{int64(5), int64(2)}, exit: 0},
		{name: "qabs", code: codeFromBuilders(t, mathop.QABS().Serialize()), stack: []any{int64(-7)}, exit: 0},

		{name: "sgn_zero", code: codeFromBuilders(t, mathop.SGN().Serialize()), stack: []any{int64(0)}, exit: 0},
		{name: "equal", code: codeFromBuilders(t, mathop.EQUAL().Serialize()), stack: []any{int64(7), int64(7)}, exit: 0},
		{name: "leq", code: codeFromBuilders(t, mathop.LEQ().Serialize()), stack: []any{int64(7), int64(7)}, exit: 0},
		{name: "greater", code: codeFromBuilders(t, mathop.GREATER().Serialize()), stack: []any{int64(9), int64(7)}, exit: 0},
		{name: "neq", code: codeFromBuilders(t, mathop.NEQ().Serialize()), stack: []any{int64(9), int64(7)}, exit: 0},
		{name: "gtint_negative", code: codeFromBuilders(t, mathop.GTINT(-5).Serialize()), stack: []any{int64(-4)}, exit: 0},
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

	runArithParityCases(t, tests)
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
