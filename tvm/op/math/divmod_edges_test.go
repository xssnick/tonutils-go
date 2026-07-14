package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func tvmEdgeMaxInt() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
}

func tvmEdgeMinInt() *big.Int {
	return new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))
}

func pushMathCoverageBigInt(t *testing.T, st *vm.State, v *big.Int) {
	t.Helper()
	if err := st.Stack.PushInt(new(big.Int).Set(v)); err != nil {
		t.Fatalf("push big int: %v", err)
	}
}

func popMathCoverageBigInt(t *testing.T, st *vm.State) *big.Int {
	t.Helper()
	v, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop big int: %v", err)
	}
	return v
}

func TestDivModMinNegativeOneParityEdges(t *testing.T) {
	minTVMInt := tvmEdgeMinInt()

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "DIV", op: DIV()},
		{name: "DIVR", op: DIVR()},
		{name: "DIVC", op: DIVC()},
		{name: "DIVMOD", op: DIVMOD()},
		{name: "DIVMODR", op: DIVMODR()},
		{name: "DIVMODC", op: DIVMODC()},
	} {
		t.Run(op.name+"_quotient_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, minTVMInt)
			pushMathCoverageInts(t, st, -1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MOD", op: MOD()},
		{name: "MODR", op: MODR()},
		{name: "MODC", op: MODC()},
	} {
		t.Run(op.name+"_wide_quotient_zero_remainder", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, minTVMInt)
			pushMathCoverageInts(t, st, -1)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s remainder = %d, want 0", op.name, got)
			}
		})
	}
}

func TestBasicDivModNaNAndZeroDivisorEdges(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "DIV", op: DIV()},
		{name: "DIVR", op: DIVR()},
		{name: "DIVC", op: DIVC()},
		{name: "MOD", op: MOD()},
		{name: "MODR", op: MODR()},
		{name: "MODC", op: MODC()},
		{name: "DIVMOD", op: DIVMOD()},
		{name: "DIVMODR", op: DIVMODR()},
		{name: "DIVMODC", op: DIVMODC()},
	} {
		t.Run(op.name+"_nan_x", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN x: %v", err)
			}
			pushMathCoverageInts(t, st, 2)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_nan_y", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN y: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_zero_divisor", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 0)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestBasicDivModErrorOrderEdges(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "DIV", op: DIV()},
		{name: "DIVR", op: DIVR()},
		{name: "DIVC", op: DIVC()},
		{name: "MOD", op: MOD()},
		{name: "MODR", op: MODR()},
		{name: "MODC", op: MODC()},
	} {
		t.Run(op.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 7)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(op.name+"_y_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 7)
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_x_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 2)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}
}

func TestMulDivAndMulModWideParityEdges(t *testing.T) {
	maxTVMInt := tvmEdgeMaxInt()

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULDIV", op: MULDIV()},
		{name: "MULDIVR", op: MULDIVR()},
		{name: "MULDIVC", op: MULDIVC()},
	} {
		t.Run(op.name+"_wide_product_valid", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2, 2)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageBigInt(t, st); got.Cmp(maxTVMInt) != 0 {
				t.Fatalf("%s quotient = %s, want %s", op.name, got, maxTVMInt)
			}
		})

		t.Run(op.name+"_wide_product_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_nan_multiplier", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN multiplier: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_zero_divisor", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2, 0)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULMOD", op: MULMOD()},
		{name: "MULMODR", op: MULMODR()},
		{name: "MULMODC", op: MULMODC()},
	} {
		t.Run(op.name+"_wide_product_zero_remainder", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2, 1)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s remainder = %d, want 0", op.name, got)
			}
		})

		t.Run(op.name+"_nan_divisor", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN divisor: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_zero_divisor", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2, 0)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func FuzzTVMBasicDivModNaNZeroAndOverflowRules(f *testing.F) {
	for _, seed := range []struct {
		opKind     uint8
		x, y       int64
		nanX, nanY bool
	}{
		{opKind: 0, x: 10, y: 3},
		{opKind: 1, x: -10, y: 3},
		{opKind: 2, x: -10, y: 3},
		{opKind: 3, x: 10, y: 3},
		{opKind: 4, x: -10, y: 3},
		{opKind: 5, x: -10, y: 3},
		{opKind: 0, x: 10, y: 0},
		{opKind: 3, x: 10, y: 0},
		{opKind: 1, x: 10, y: 3, nanX: true},
		{opKind: 4, x: 10, y: 3, nanY: true},
	} {
		f.Add(seed.opKind, seed.x, seed.y, seed.nanX, seed.nanY)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawY int64, nanX, nanY bool) {
		opKind := rawOp % 6
		st := newMathCoverageState()

		x := big.NewInt(rawX % 1_000_000)
		y := big.NewInt(rawY % 1_000_000)
		if rawX%17 == 0 {
			x = tvmEdgeMaxInt()
		}
		if rawX%19 == 0 {
			x = tvmEdgeMinInt()
		}
		if rawY%23 == 0 {
			y = big.NewInt(-1)
		}

		pushMathCoverageMaybeNaN(t, st, x, nanX)
		pushMathCoverageMaybeNaN(t, st, y, nanY)

		var err error
		switch opKind {
		case 0:
			err = DIV().Interpret(st)
		case 1:
			err = DIVR().Interpret(st)
		case 2:
			err = DIVC().Interpret(st)
		case 3:
			err = MOD().Interpret(st)
		case 4:
			err = MODR().Interpret(st)
		default:
			err = MODC().Interpret(st)
		}

		if nanX || nanY || y.Sign() == 0 {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		divOp := opKind < 3
		if divOp && x.Cmp(tvmEdgeMinInt()) == 0 && y.Cmp(bigIntMinusOne) == 0 {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		if err != nil {
			t.Fatalf("basic div/mod op failed: %v", err)
		}

		got, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop div/mod result: %v", err)
		}
		v, ok := got.(*big.Int)
		if !ok {
			t.Fatalf("result type = %T, want *big.Int", got)
		}
		if !signedFitsBits(v, 257) {
			t.Fatalf("finite div/mod result out of TVM range: %s", v)
		}
	})
}

func TestAddAndMulDivModWideParityEdges(t *testing.T) {
	maxTVMInt := tvmEdgeMaxInt()

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "ADDDIVMOD", op: ADDDIVMOD()},
		{name: "ADDDIVMODR", op: ADDDIVMODR()},
		{name: "ADDDIVMODC", op: ADDDIVMODC()},
	} {
		t.Run(op.name+"_wide_sum_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 1, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_zero_divisor", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 1, 0)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_nan_divisor", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 1)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN divisor: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULDIVMOD", op: MULDIVMOD()},
		{name: "MULDIVMODR", op: MULDIVMODR()},
		{name: "MULDIVMODC", op: MULDIVMODC()},
	} {
		t.Run(op.name+"_wide_product_valid", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2, 2)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s remainder = %d, want 0", op.name, got)
			}
			if got := popMathCoverageBigInt(t, st); got.Cmp(maxTVMInt) != 0 {
				t.Fatalf("%s quotient = %s, want %s", op.name, got, maxTVMInt)
			}
		})

		t.Run(op.name+"_wide_product_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestMulAddDivModWideAndBadOperandEdges(t *testing.T) {
	maxTVMInt := tvmEdgeMaxInt()

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULADDDIVMOD", op: MULADDDIVMOD()},
		{name: "MULADDDIVMODR", op: MULADDDIVMODR()},
		{name: "MULADDDIVMODC", op: MULADDDIVMODC()},
	} {
		t.Run(op.name+"_nan_operands", func(t *testing.T) {
			for nanSlot := 0; nanSlot < 4; nanSlot++ {
				st := newMathCoverageState()
				for idx, value := range []int64{5, 2, 1, 1} {
					if idx == nanSlot {
						if err := st.Stack.PushAny(vm.NaN{}); err != nil {
							t.Fatalf("push NaN slot %d: %v", nanSlot, err)
						}
						continue
					}
					pushMathCoverageInts(t, st, value)
				}
				assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
			}
		})

		t.Run(op.name+"_zero_divisor", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2, 1, 0)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_wide_exact_valid", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2, 0, 2)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s remainder = %d, want 0", op.name, got)
			}
			if got := popMathCoverageBigInt(t, st); got.Cmp(maxTVMInt) != 0 {
				t.Fatalf("%s quotient = %s, want %s", op.name, got, maxTVMInt)
			}
		})

		t.Run(op.name+"_wide_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2, 0, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestLShiftAddDivModRangeAndWideEdges(t *testing.T) {
	maxTVMInt := tvmEdgeMaxInt()

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "LSHIFTADDDIVMOD", op: LSHIFTADDDIVMOD()},
		{name: "LSHIFTADDDIVMODR", op: LSHIFTADDDIVMODR()},
		{name: "LSHIFTADDDIVMODC", op: LSHIFTADDDIVMODC()},
	} {
		t.Run(op.name+"_nan_shift_range", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 1, 2)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN shift: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})

		t.Run(op.name+"_bad_shift_beats_nan_value", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN value: %v", err)
			}
			pushMathCoverageInts(t, st, 1, 2, 257)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})

		t.Run(op.name+"_nan_finite_operands", func(t *testing.T) {
			for nanSlot := 0; nanSlot < 3; nanSlot++ {
				st := newMathCoverageState()
				for idx, value := range []int64{5, 1, 2} {
					if idx == nanSlot {
						if err := st.Stack.PushAny(vm.NaN{}); err != nil {
							t.Fatalf("push NaN slot %d: %v", nanSlot, err)
						}
						continue
					}
					pushMathCoverageInts(t, st, value)
				}
				pushMathCoverageInts(t, st, 1)
				assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
			}
		})

		t.Run(op.name+"_zero_divisor", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 1, 0, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_wide_exact_valid", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 0, 2, 1)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s remainder = %d, want 0", op.name, got)
			}
			if got := popMathCoverageBigInt(t, st); got.Cmp(maxTVMInt) != 0 {
				t.Fatalf("%s quotient = %s, want %s", op.name, got, maxTVMInt)
			}
		})

		t.Run(op.name+"_wide_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 0, 1, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestAddrShiftModRangeAndWideEdges(t *testing.T) {
	maxTVMInt := tvmEdgeMaxInt()
	halfPow256 := new(big.Int).Lsh(big.NewInt(1), 255)

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "ADDRSHIFTMOD", op: ADDRSHIFTMOD()},
		{name: "ADDRSHIFTMODR", op: ADDRSHIFTMODR()},
		{name: "ADDRSHIFTMODC", op: ADDRSHIFTMODC()},
	} {
		t.Run(op.name+"_nan_shift_range", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 1)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN shift: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})

		t.Run(op.name+"_bad_shift_beats_nan_value", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN value: %v", err)
			}
			pushMathCoverageInts(t, st, 1, 257)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})

		t.Run(op.name+"_nan_operands", func(t *testing.T) {
			for nanSlot := 0; nanSlot < 2; nanSlot++ {
				st := newMathCoverageState()
				for idx, value := range []int64{5, 1} {
					if idx == nanSlot {
						if err := st.Stack.PushAny(vm.NaN{}); err != nil {
							t.Fatalf("push NaN slot %d: %v", nanSlot, err)
						}
						continue
					}
					pushMathCoverageInts(t, st, value)
				}
				pushMathCoverageInts(t, st, 1)
				assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
			}
		})

		t.Run(op.name+"_wide_valid", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 1, 1)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s remainder = %d, want 0", op.name, got)
			}
			if got := popMathCoverageBigInt(t, st); got.Cmp(halfPow256) != 0 {
				t.Fatalf("%s quotient = %s, want %s", op.name, got, halfPow256)
			}
		})

		t.Run(op.name+"_wide_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 1, 0)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestMulAddrShiftModRangeAndWideEdges(t *testing.T) {
	maxTVMInt := tvmEdgeMaxInt()

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULADDRSHIFTMOD", op: MULADDRSHIFTMOD()},
		{name: "MULADDRSHIFTRMOD", op: MULADDRSHIFTRMOD()},
		{name: "MULADDRSHIFTCMOD", op: MULADDRSHIFTCMOD()},
	} {
		t.Run(op.name+"_nan_shift_range", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2, 1)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN shift: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})

		t.Run(op.name+"_bad_shift_beats_nan_value", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN value: %v", err)
			}
			pushMathCoverageInts(t, st, 2, 1, 257)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})

		t.Run(op.name+"_nan_operands", func(t *testing.T) {
			for nanSlot := 0; nanSlot < 3; nanSlot++ {
				st := newMathCoverageState()
				for idx, value := range []int64{5, 2, 1} {
					if idx == nanSlot {
						if err := st.Stack.PushAny(vm.NaN{}); err != nil {
							t.Fatalf("push NaN slot %d: %v", nanSlot, err)
						}
						continue
					}
					pushMathCoverageInts(t, st, value)
				}
				pushMathCoverageInts(t, st, 1)
				assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
			}
		})

		t.Run(op.name+"_wide_valid", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2, 0, 1)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s remainder = %d, want 0", op.name, got)
			}
			if got := popMathCoverageBigInt(t, st); got.Cmp(maxTVMInt) != 0 {
				t.Fatalf("%s quotient = %s, want %s", op.name, got, maxTVMInt)
			}
		})

		t.Run(op.name+"_wide_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2, 0, 0)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestImmediateAddrShiftModWideEdges(t *testing.T) {
	maxTVMInt := tvmEdgeMaxInt()

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "ADDRSHIFT#MOD", op: ADDRSHIFTCODEMOD(0)},
		{name: "ADDRSHIFTR#MOD", op: ADDRSHIFTRCODEMOD(0)},
		{name: "ADDRSHIFTC#MOD", op: ADDRSHIFTCCODEMOD(0)},
	} {
		t.Run(op.name+"_wide_valid", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageBigInt(t, st, maxTVMInt)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s remainder = %d, want 0", op.name, got)
			}
			if got := popMathCoverageBigInt(t, st); got.Cmp(maxTVMInt) != 0 {
				t.Fatalf("%s quotient = %s, want %s", op.name, got, maxTVMInt)
			}
		})
	}

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULADDRSHIFT#MOD", op: MULADDRSHIFTCODEMOD(0)},
		{name: "MULADDRSHIFTR#MOD", op: MULADDRSHIFTRCODEMOD(0)},
		{name: "MULADDRSHIFTC#MOD", op: MULADDRSHIFTCCODEMOD(0)},
	} {
		t.Run(op.name+"_wide_valid", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2, 0)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s remainder = %d, want 0", op.name, got)
			}
			if got := popMathCoverageBigInt(t, st); got.Cmp(maxTVMInt) != 0 {
				t.Fatalf("%s quotient = %s, want %s", op.name, got, maxTVMInt)
			}
		})

		t.Run(op.name+"_wide_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 3, 0)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestLShiftDivModRangeAndWideParityEdges(t *testing.T) {
	maxTVMInt := tvmEdgeMaxInt()

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "LSHIFTDIV", op: LSHIFTDIV()},
		{name: "LSHIFTDIVR", op: LSHIFTDIVR()},
		{name: "LSHIFTDIVC", op: LSHIFTDIVC()},
		{name: "LSHIFTMOD", op: LSHIFTMOD()},
		{name: "LSHIFTMODR", op: LSHIFTMODR()},
		{name: "LSHIFTMODC", op: LSHIFTMODC()},
		{name: "LSHIFTDIVMOD", op: LSHIFTDIVMOD()},
		{name: "LSHIFTDIVMODR", op: LSHIFTDIVMODR()},
		{name: "LSHIFTDIVMODC", op: LSHIFTDIVMODC()},
	} {
		t.Run(op.name+"_nan_shift_range", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN shift: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})

		t.Run(op.name+"_bad_shift_beats_nan_value", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN value: %v", err)
			}
			pushMathCoverageInts(t, st, 2, 257)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})
	}

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "LSHIFTDIV", op: LSHIFTDIV()},
		{name: "LSHIFTDIVR", op: LSHIFTDIVR()},
		{name: "LSHIFTDIVC", op: LSHIFTDIVC()},
		{name: "LSHIFTDIVMOD", op: LSHIFTDIVMOD()},
		{name: "LSHIFTDIVMODR", op: LSHIFTDIVMODR()},
		{name: "LSHIFTDIVMODC", op: LSHIFTDIVMODC()},
	} {
		t.Run(op.name+"_wide_shift_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 1, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "LSHIFTMOD", op: LSHIFTMOD()},
		{name: "LSHIFTMODR", op: LSHIFTMODR()},
		{name: "LSHIFTMODC", op: LSHIFTMODC()},
	} {
		t.Run(op.name+"_wide_shift_zero_remainder", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 1, 1)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s remainder = %d, want 0", op.name, got)
			}
		})
	}
}

func FuzzTVMCompoundDivModBadOperandRules(f *testing.F) {
	for group := uint8(0); group < 3; group++ {
		for variant := uint8(0); variant < 3; variant++ {
			for nanSlot := uint8(0); nanSlot < 4; nanSlot++ {
				f.Add(group, variant, nanSlot, uint16(0))
				f.Add(group, variant, nanSlot, uint16(1))
				f.Add(group, variant, nanSlot, uint16(257))
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawGroup, rawVariant, rawNaNSlot uint8, rawShift uint16) {
		group := rawGroup % 3
		variant := rawVariant % 3
		nanSlot := rawNaNSlot % 4
		shift := int64(rawShift % 300)

		st := newMathCoverageState()
		if nanSlot == 0 {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN x: %v", err)
			}
		} else {
			pushMathCoverageInts(t, st, 5)
		}
		if nanSlot == 1 {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN y: %v", err)
			}
		} else if group == 2 {
			pushMathCoverageInts(t, st, 0)
		} else {
			pushMathCoverageInts(t, st, 1)
		}
		if nanSlot == 2 {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN divisor/shift: %v", err)
			}
		} else if group == 2 {
			pushMathCoverageInts(t, st, shift)
		} else {
			pushMathCoverageInts(t, st, 0)
		}

		var err error
		switch {
		case group == 0 && variant == 0:
			err = ADDDIVMOD().Interpret(st)
		case group == 0 && variant == 1:
			err = ADDDIVMODR().Interpret(st)
		case group == 0:
			err = ADDDIVMODC().Interpret(st)
		case group == 1 && variant == 0:
			err = MULDIVMOD().Interpret(st)
		case group == 1 && variant == 1:
			err = MULDIVMODR().Interpret(st)
		case group == 1:
			err = MULDIVMODC().Interpret(st)
		case variant == 0:
			err = LSHIFTDIVMOD().Interpret(st)
		case variant == 1:
			err = LSHIFTDIVMODR().Interpret(st)
		default:
			err = LSHIFTDIVMODC().Interpret(st)
		}

		if group == 2 && (nanSlot == 2 || shift > 256) {
			fuzzMathExpectVMError(t, err, vmerr.CodeRangeCheck)
			return
		}
		fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
	})
}

func FuzzTVMMulAddAndLShiftAddDivModBadOperandRules(f *testing.F) {
	for group := uint8(0); group < 2; group++ {
		for variant := uint8(0); variant < 3; variant++ {
			for nanSlot := uint8(0); nanSlot < 4; nanSlot++ {
				f.Add(group, variant, nanSlot, uint16(0))
				f.Add(group, variant, nanSlot, uint16(1))
				f.Add(group, variant, nanSlot, uint16(257))
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawGroup, rawVariant, rawNaNSlot uint8, rawShift uint16) {
		group := rawGroup % 2
		variant := rawVariant % 3
		nanSlot := rawNaNSlot % 4
		shift := int64(rawShift % 300)

		st := newMathCoverageState()
		if group == 0 {
			for idx, value := range []int64{5, 2, 1, 0} {
				if idx == int(nanSlot) {
					if err := st.Stack.PushAny(vm.NaN{}); err != nil {
						t.Fatalf("push NaN slot %d: %v", nanSlot, err)
					}
					continue
				}
				pushMathCoverageInts(t, st, value)
			}
		} else {
			if nanSlot == 0 {
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN value: %v", err)
				}
			} else {
				pushMathCoverageInts(t, st, 5)
			}
			if nanSlot == 1 {
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN addend: %v", err)
				}
			} else {
				pushMathCoverageInts(t, st, 1)
			}
			if nanSlot == 2 {
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN divisor: %v", err)
				}
			} else {
				pushMathCoverageInts(t, st, 0)
			}
			if nanSlot == 3 {
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN shift: %v", err)
				}
			} else {
				pushMathCoverageInts(t, st, shift)
			}
		}

		var err error
		switch {
		case group == 0 && variant == 0:
			err = MULADDDIVMOD().Interpret(st)
		case group == 0 && variant == 1:
			err = MULADDDIVMODR().Interpret(st)
		case group == 0:
			err = MULADDDIVMODC().Interpret(st)
		case variant == 0:
			err = LSHIFTADDDIVMOD().Interpret(st)
		case variant == 1:
			err = LSHIFTADDDIVMODR().Interpret(st)
		default:
			err = LSHIFTADDDIVMODC().Interpret(st)
		}

		if group == 1 && (nanSlot == 3 || shift > 256) {
			fuzzMathExpectVMError(t, err, vmerr.CodeRangeCheck)
			return
		}
		fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
	})
}

func FuzzTVMAddrShiftModBadOperandRules(f *testing.F) {
	for group := uint8(0); group < 4; group++ {
		for variant := uint8(0); variant < 3; variant++ {
			for nanSlot := uint8(0); nanSlot < 4; nanSlot++ {
				f.Add(group, variant, nanSlot, uint16(0))
				f.Add(group, variant, nanSlot, uint16(1))
				f.Add(group, variant, nanSlot, uint16(257))
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawGroup, rawVariant, rawNaNSlot uint8, rawShift uint16) {
		group := rawGroup % 4
		variant := rawVariant % 3
		shift := int64(rawShift % 300)
		dynamicBadShift := false

		st := newMathCoverageState()
		switch group {
		case 0:
			nanSlot := rawNaNSlot % 3
			dynamicBadShift = nanSlot == 2
			for idx, value := range []int64{5, 1, shift} {
				if idx == int(nanSlot) {
					if err := st.Stack.PushAny(vm.NaN{}); err != nil {
						t.Fatalf("push NaN slot %d: %v", nanSlot, err)
					}
					continue
				}
				pushMathCoverageInts(t, st, value)
			}
		case 1:
			nanSlot := rawNaNSlot % 4
			dynamicBadShift = nanSlot == 3
			for idx, value := range []int64{5, 2, 1, shift} {
				if idx == int(nanSlot) {
					if err := st.Stack.PushAny(vm.NaN{}); err != nil {
						t.Fatalf("push NaN slot %d: %v", nanSlot, err)
					}
					continue
				}
				pushMathCoverageInts(t, st, value)
			}
		case 2:
			nanSlot := rawNaNSlot % 2
			for idx, value := range []int64{5, 1} {
				if idx == int(nanSlot) {
					if err := st.Stack.PushAny(vm.NaN{}); err != nil {
						t.Fatalf("push NaN slot %d: %v", nanSlot, err)
					}
					continue
				}
				pushMathCoverageInts(t, st, value)
			}
		default:
			nanSlot := rawNaNSlot % 3
			for idx, value := range []int64{5, 2, 1} {
				if idx == int(nanSlot) {
					if err := st.Stack.PushAny(vm.NaN{}); err != nil {
						t.Fatalf("push NaN slot %d: %v", nanSlot, err)
					}
					continue
				}
				pushMathCoverageInts(t, st, value)
			}
		}

		var err error
		switch {
		case group == 0 && variant == 0:
			err = ADDRSHIFTMOD().Interpret(st)
		case group == 0 && variant == 1:
			err = ADDRSHIFTMODR().Interpret(st)
		case group == 0:
			err = ADDRSHIFTMODC().Interpret(st)
		case group == 1 && variant == 0:
			err = MULADDRSHIFTMOD().Interpret(st)
		case group == 1 && variant == 1:
			err = MULADDRSHIFTRMOD().Interpret(st)
		case group == 1:
			err = MULADDRSHIFTCMOD().Interpret(st)
		case group == 2 && variant == 0:
			err = ADDRSHIFTCODEMOD(int8(rawShift)).Interpret(st)
		case group == 2 && variant == 1:
			err = ADDRSHIFTRCODEMOD(int8(rawShift)).Interpret(st)
		case group == 2:
			err = ADDRSHIFTCCODEMOD(int8(rawShift)).Interpret(st)
		case variant == 0:
			err = MULADDRSHIFTCODEMOD(int8(rawShift)).Interpret(st)
		case variant == 1:
			err = MULADDRSHIFTRCODEMOD(int8(rawShift)).Interpret(st)
		default:
			err = MULADDRSHIFTCCODEMOD(int8(rawShift)).Interpret(st)
		}

		if group < 2 {
			if dynamicBadShift || shift > 256 {
				fuzzMathExpectVMError(t, err, vmerr.CodeRangeCheck)
				return
			}
		}
		fuzzMathExpectVMError(t, err, vmerr.CodeIntOverflow)
	})
}
