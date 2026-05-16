package tvm

import (
	"math/big"
	"testing"

	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestArithOpsGoSemantics(t *testing.T) {
	t.Run("PushConstAndPushNanDecode", func(t *testing.T) {
		stack, res, err := runRawCode(codeFromBuilders(t, mathop.PUSHPOW2(4).Serialize()))
		if err != nil {
			t.Fatalf("pushpow2 unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("pushpow2 unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pushpow2 pop result: %v", err)
		}
		if got.Cmp(new(big.Int).Lsh(big.NewInt(1), 5)) != 0 {
			t.Fatalf("unexpected pushpow2 result: %s", got.String())
		}

		stack, res, err = runRawCode(codeFromOpcodes(t, 0x83FF))
		if err != nil {
			t.Fatalf("pushnan unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("pushnan unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		nan, err := stack.PopInt()
		if err != nil {
			t.Fatalf("pushnan pop result: %v", err)
		}
		if nan != nil {
			t.Fatalf("expected NaN from PUSHNAN, got %s", nan.String())
		}
	})

	t.Run("QuietAddFamilyPropagatesNaNAndOverflow", func(t *testing.T) {
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

		stack, res, err := runRawCode(codeFromBuilders(t, mathop.QADD().Serialize()), max, int64(1))
		if err != nil {
			t.Fatalf("qadd unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("qadd unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		nan, err := stack.PopInt()
		if err != nil {
			t.Fatalf("qadd pop result: %v", err)
		}
		if nan != nil {
			t.Fatalf("expected NaN after qadd overflow, got %s", nan.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.QADDINT(-5).Serialize()), int64(9))
		if err != nil {
			t.Fatalf("qaddint unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("qaddint unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("qaddint pop result: %v", err)
		}
		if got.Int64() != 4 {
			t.Fatalf("unexpected qaddint result: %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.QMULINT(-3).Serialize()), int64(7))
		if err != nil {
			t.Fatalf("qmulint unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("qmulint unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("qmulint pop result: %v", err)
		}
		if got.Int64() != -21 {
			t.Fatalf("unexpected qmulint result: %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.QNOT().Serialize()), vm.NaN{})
		if err != nil {
			t.Fatalf("qnot unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("qnot unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		nan, err = stack.PopInt()
		if err != nil {
			t.Fatalf("qnot pop result: %v", err)
		}
		if nan != nil {
			t.Fatalf("expected NaN after qnot on NaN input, got %s", nan.String())
		}
	})

	t.Run("FitsBitsizeAndNanChecks", func(t *testing.T) {
		_, res, err := runRawCode(codeFromBuilders(t, mathop.FITS(7).Serialize()), int64(128))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeIntOverflow {
			t.Fatalf("fits expected int overflow, got %d err=%v", code, err)
		}

		stack, res, err := runRawCode(codeFromBuilders(t, mathop.QFITS(7).Serialize()), int64(128))
		if err != nil {
			t.Fatalf("qfits unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("qfits unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		nan, err := stack.PopInt()
		if err != nil {
			t.Fatalf("qfits pop result: %v", err)
		}
		if nan != nil {
			t.Fatalf("expected NaN from qfits overflow, got %s", nan.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.BITSIZE().Serialize()), int64(-3))
		if err != nil {
			t.Fatalf("bitsize unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("bitsize unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("bitsize pop result: %v", err)
		}
		if got.Int64() != 3 {
			t.Fatalf("unexpected bitsize result: %s", got.String())
		}

		_, res, err = runRawCode(codeFromBuilders(t, mathop.UBITSIZE().Serialize()), int64(-1))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("ubitsize expected range check, got %d err=%v", code, err)
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.QUBITSIZE().Serialize()), int64(-1))
		if err != nil {
			t.Fatalf("qubitsize unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("qubitsize unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		nan, err = stack.PopInt()
		if err != nil {
			t.Fatalf("qubitsize pop result: %v", err)
		}
		if nan != nil {
			t.Fatalf("expected NaN from qubitsize on negative input, got %s", nan.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.ISNAN().Serialize()), vm.NaN{})
		if err != nil {
			t.Fatalf("isnan unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("isnan unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		isNaN, err := stack.PopBool()
		if err != nil {
			t.Fatalf("isnan pop result: %v", err)
		}
		if !isNaN {
			t.Fatal("expected ISNAN to return true")
		}

		_, res, err = runRawCode(codeFromBuilders(t, mathop.CHKNAN().Serialize()), vm.NaN{})
		if code := exitCodeFromResult(res, err); code != vmerr.CodeIntOverflow {
			t.Fatalf("chknan expected int overflow, got %d err=%v", code, err)
		}
	})

	t.Run("MinMaxAndCompareFamilies", func(t *testing.T) {
		stack, res, err := runRawCode(codeFromBuilders(t, mathop.MINMAX().Serialize()), int64(5), int64(2))
		if err != nil {
			t.Fatalf("minmax unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("minmax unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		max, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("minmax pop max: %v", err)
		}
		min, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("minmax pop min: %v", err)
		}
		if min.Int64() != 2 || max.Int64() != 5 {
			t.Fatalf("unexpected minmax results: min=%s max=%s", min.String(), max.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.QMINMAX().Serialize()), vm.NaN{}, int64(5))
		if err != nil {
			t.Fatalf("qminmax unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("qminmax unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		n1, err := stack.PopInt()
		if err != nil {
			t.Fatalf("qminmax pop first: %v", err)
		}
		n2, err := stack.PopInt()
		if err != nil {
			t.Fatalf("qminmax pop second: %v", err)
		}
		if n1 != nil || n2 != nil {
			t.Fatalf("expected NaN pair from qminmax, got %v and %v", n1, n2)
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.SGN().Serialize()), int64(-7))
		if err != nil {
			t.Fatalf("sgn unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("sgn unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("sgn pop result: %v", err)
		}
		if got.Int64() != -1 {
			t.Fatalf("unexpected sgn result: %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.CMP().Serialize()), int64(2), int64(5))
		if err != nil {
			t.Fatalf("cmp unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("cmp unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("cmp pop result: %v", err)
		}
		if got.Int64() != -1 {
			t.Fatalf("unexpected cmp result: %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.QSGN().Serialize()), vm.NaN{})
		if err != nil {
			t.Fatalf("qsgn unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("qsgn unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		nan, err := stack.PopInt()
		if err != nil {
			t.Fatalf("qsgn pop result: %v", err)
		}
		if nan != nil {
			t.Fatalf("expected NaN from qsgn on NaN input, got %s", nan.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.QCMP().Serialize()), vm.NaN{}, int64(1))
		if err != nil {
			t.Fatalf("qcmp unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("qcmp unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		nan, err = stack.PopInt()
		if err != nil {
			t.Fatalf("qcmp pop result: %v", err)
		}
		if nan != nil {
			t.Fatalf("expected NaN from qcmp on NaN input, got %s", nan.String())
		}
	})

	t.Run("ImmediateFamiliesAndCompareOrder", func(t *testing.T) {
		stack, res, err := runRawCode(codeFromBuilders(t, mathop.ADDINT(-5).Serialize()), int64(9))
		if err != nil {
			t.Fatalf("addint unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("addint unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("addint pop result: %v", err)
		}
		if got.Int64() != 4 {
			t.Fatalf("unexpected addint result: %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.MULINT(-3).Serialize()), int64(7))
		if err != nil {
			t.Fatalf("mulint unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("mulint unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		got, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("mulint pop result: %v", err)
		}
		if got.Int64() != -21 {
			t.Fatalf("unexpected mulint result: %s", got.String())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.EQINT(-5).Serialize()), int64(-5))
		if err != nil {
			t.Fatalf("eqint unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("eqint unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		equal, err := stack.PopBool()
		if err != nil {
			t.Fatalf("eqint pop result: %v", err)
		}
		if !equal {
			t.Fatal("expected eqint(-5) to match -5")
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.LESSINT(-5).Serialize()), int64(-6))
		if err != nil {
			t.Fatalf("lessint unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("lessint unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		less, err := stack.PopBool()
		if err != nil {
			t.Fatalf("lessint pop result: %v", err)
		}
		if !less {
			t.Fatal("expected lessint(-5) to treat -6 < -5")
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.NEQINT(-5).Serialize()), int64(-4))
		if err != nil {
			t.Fatalf("neqint unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("neqint unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		notEqual, err := stack.PopBool()
		if err != nil {
			t.Fatalf("neqint pop result: %v", err)
		}
		if !notEqual {
			t.Fatal("expected neqint(-5) to treat -4 != -5")
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.LESS().Serialize()), int64(1), int64(2))
		if err != nil {
			t.Fatalf("less unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("less unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		less, err = stack.PopBool()
		if err != nil {
			t.Fatalf("less pop result: %v", err)
		}
		if !less {
			t.Fatal("expected less to use TVM stack order")
		}

		stack, res, err = runRawCode(codeFromBuilders(t, mathop.GEQ().Serialize()), int64(3), int64(2))
		if err != nil {
			t.Fatalf("geq unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("geq unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack

		geq, err := stack.PopBool()
		if err != nil {
			t.Fatalf("geq pop result: %v", err)
		}
		if !geq {
			t.Fatal("expected geq to use TVM stack order")
		}
	})
}
