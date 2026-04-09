//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorArithOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

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
		{name: "addint_negative", code: codeFromBuilders(t, mathop.ADDINT(-5).Serialize()), stack: []any{int64(9)}, exit: 0},
		{name: "mulint_negative", code: codeFromBuilders(t, mathop.MULINT(-3).Serialize()), stack: []any{int64(7)}, exit: 0},
		{name: "qadd_overflow", code: codeFromBuilders(t, mathop.QADD().Serialize()), stack: []any{maxTVMInt, int64(1)}, exit: 0},
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
		{name: "qpow2_overflow", code: codeFromBuilders(t, mathop.QPOW2().Serialize()), stack: []any{int64(300)}, exit: 0},
		{name: "fits_fail", code: codeFromBuilders(t, mathop.FITS(6).Serialize()), stack: []any{int64(64)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "ufits_fail", code: codeFromBuilders(t, mathop.UFITS(6).Serialize()), stack: []any{int64(-1)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "bitsize_negative", code: codeFromBuilders(t, mathop.BITSIZE().Serialize()), stack: []any{int64(-3)}, exit: 0},
		{name: "ubitsize_negative", code: codeFromBuilders(t, mathop.UBITSIZE().Serialize()), stack: []any{int64(-1)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "qfits_fail", code: codeFromBuilders(t, mathop.QFITS(7).Serialize()), stack: []any{int64(128)}, exit: 0},
		{name: "fitsx_fail", code: codeFromBuilders(t, mathop.FITSX().Serialize()), stack: []any{int64(128), int64(7)}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "qfitsx_fail", code: codeFromBuilders(t, mathop.QFITSX().Serialize()), stack: []any{int64(128), int64(8)}, exit: 0},
		{name: "qufits_fail", code: codeFromBuilders(t, mathop.QUFITS(7).Serialize()), stack: []any{int64(-1)}, exit: 0},
		{name: "qufitsx_fail", code: codeFromBuilders(t, mathop.QUFITSX().Serialize()), stack: []any{int64(-1), int64(8)}, exit: 0},
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
