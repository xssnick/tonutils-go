package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestLShiftDivSingleErrorEdges(t *testing.T) {
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
	} {
		t.Run(op.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(op.name+"_shift_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2)
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_divisor_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_value_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 2, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_divisor_nan", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push divisor NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_value_nan", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push value NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 2, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_zero_divisor", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 0, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestLShiftDivSingleRoundingAndModuloEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		want int64
	}{
		{name: "LSHIFTDIVFloor", op: LSHIFTDIV(), want: 2},
		{name: "LSHIFTDIVRRound", op: LSHIFTDIVR(), want: 3},
		{name: "LSHIFTDIVCCeil", op: LSHIFTDIVC(), want: 3},
		{name: "LSHIFTMODFloor", op: LSHIFTMOD(), want: 2},
		{name: "LSHIFTMODRRound", op: LSHIFTMODR(), want: -2},
		{name: "LSHIFTMODCCeil", op: LSHIFTMODC(), want: -2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 4, 1)
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s interpret: %v", tt.name, err)
			}
			if got := popMathCoverageInt(t, st); got != tt.want {
				t.Fatalf("%s result = %d, want %d", tt.name, got, tt.want)
			}
		})
	}

	t.Run("LSHIFTDIVWideShiftOneFits", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageBigInt(t, st, tvmEdgeMaxInt())
		pushMathCoverageInts(t, st, 2, 1)

		if err := LSHIFTDIV().Interpret(st); err != nil {
			t.Fatalf("LSHIFTDIV interpret: %v", err)
		}
		if got := popMathCoverageBigInt(t, st); got.Cmp(tvmEdgeMaxInt()) != 0 {
			t.Fatalf("LSHIFTDIV result = %s, want max TVM int", got)
		}
	})
}

func TestLShiftDivCodeErrorAndBoundaryEdges(t *testing.T) {
	t.Run("AddDivModMinVersionAndText", func(t *testing.T) {
		op := lshiftDivCodeOp("LSHIFTADDDIVMOD#", 0xD0, 0, 0, 2)
		if got := op.MinGlobalVersion(); got != 4 {
			t.Fatalf("min version = %d, want 4", got)
		}
		if got := op.SerializeText(); got != "2 LSHIFTADDDIVMOD#" {
			t.Fatalf("text = %q, want %q", got, "2 LSHIFTADDDIVMOD#")
		}
	})

	t.Run("AddDivModUnderflow", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 5, 2)
		assertMathCoverageVMError(t, lshiftDivCodeOp("TEST", 0xD0, 0, 0, 1).Interpret(st), vmerr.CodeStackUnderflow)
	})

	t.Run("ShiftType", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 5, 3)
		pushMathCoverageNonInt(t, st)
		assertMathCoverageVMError(t, lshiftDivCodeOp("TEST", 0xD4, 1, 0, 1).Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("AddDivModAddendType", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 5)
		pushMathCoverageNonInt(t, st)
		pushMathCoverageInts(t, st, 3)
		assertMathCoverageVMError(t, lshiftDivCodeOp("TEST", 0xD0, 0, 0, 1).Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("AddDivModValueType", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageNonInt(t, st)
		pushMathCoverageInts(t, st, 1, 3)
		assertMathCoverageVMError(t, lshiftDivCodeOp("TEST", 0xD0, 0, 0, 1).Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("AddDivModAddendNaN", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 5)
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push addend NaN: %v", err)
		}
		pushMathCoverageInts(t, st, 3)
		assertMathCoverageVMError(t, lshiftDivCodeOp("TEST", 0xD0, 0, 0, 1).Interpret(st), vmerr.CodeIntOverflow)
	})

	t.Run("DivShiftNaN", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 5)
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push divisor NaN: %v", err)
		}
		assertMathCoverageVMError(t, lshiftDivCodeOp("TEST", 0xD4, 1, 0, 1).Interpret(st), vmerr.CodeIntOverflow)
	})

	t.Run("ZeroDivisor", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 5, 0)
		assertMathCoverageVMError(t, lshiftDivCodeOp("TEST", 0xD4, 1, 0, 1).Interpret(st), vmerr.CodeIntOverflow)
	})

	t.Run("AddDivModResult", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 5, 3, 4)
		if err := lshiftDivCodeOp("TEST", 0xD0, 0, 0, 1).Interpret(st); err != nil {
			t.Fatalf("LSHIFTADDDIVMOD# interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 1 {
			t.Fatalf("remainder = %d, want 1", got)
		}
		if got := popMathCoverageInt(t, st); got != 3 {
			t.Fatalf("quotient = %d, want 3", got)
		}
	})

	t.Run("ModResult", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 5, 4)
		if err := lshiftDivCodeOp("TEST", 0xD8, 2, 0, 1).Interpret(st); err != nil {
			t.Fatalf("LSHIFTMOD# interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 2 {
			t.Fatalf("remainder = %d, want 2", got)
		}
	})

	t.Run("AddDivModQuotientOverflow", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageBigInt(t, st, tvmEdgeMaxInt())
		pushMathCoverageInts(t, st, 1, 1)
		assertMathCoverageVMError(t, lshiftDivCodeOp("TEST", 0xD0, 0, 0, 1).Interpret(st), vmerr.CodeIntOverflow)
	})
}

func FuzzTVMLShiftDivSingleNaNRangeAndOverflowRules(f *testing.F) {
	for _, seed := range []struct {
		opKind               uint8
		x, y                 int64
		shift                uint16
		nanX, nanY, nanShift bool
	}{
		{opKind: 0, x: 5, y: 4, shift: 1},
		{opKind: 1, x: -5, y: 4, shift: 1},
		{opKind: 2, x: 5, y: -4, shift: 1},
		{opKind: 3, x: 5, y: 4, shift: 1},
		{opKind: 4, x: -5, y: 4, shift: 1},
		{opKind: 5, x: 5, y: -4, shift: 1},
		{opKind: 0, x: 5, y: 4, shift: 257},
		{opKind: 2, x: 5, y: 4, nanShift: true},
		{opKind: 3, x: 5, y: 0, shift: 1},
		{opKind: 4, x: 5, y: 4, nanY: true},
		{opKind: 5, x: 5, y: 4, nanX: true},
	} {
		f.Add(seed.opKind, seed.x, seed.y, seed.shift, seed.nanX, seed.nanY, seed.nanShift)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawY int64, rawShift uint16, nanX, nanY, nanShift bool) {
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
		if rawY%31 == 0 {
			y = big.NewInt(0)
		} else if rawY%23 == 0 {
			y = tvmEdgeMaxInt()
		} else if rawY%29 == 0 {
			y = tvmEdgeMinInt()
		}

		pushMathCoverageMaybeNaN(t, st, x, nanX)
		pushMathCoverageMaybeNaN(t, st, y, nanY)
		if nanShift {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push shift NaN: %v", err)
			}
		} else if err := st.Stack.PushInt(big.NewInt(int64(rawShift))); err != nil {
			t.Fatalf("push shift: %v", err)
		}

		var err error
		switch opKind {
		case 0:
			err = LSHIFTDIV().Interpret(st)
		case 1:
			err = LSHIFTDIVR().Interpret(st)
		case 2:
			err = LSHIFTDIVC().Interpret(st)
		case 3:
			err = LSHIFTMOD().Interpret(st)
		case 4:
			err = LSHIFTMODR().Interpret(st)
		default:
			err = LSHIFTMODC().Interpret(st)
		}

		if nanShift || rawShift > 256 {
			assertMathCoverageVMError(t, err, vmerr.CodeRangeCheck)
			return
		}
		if nanX || nanY || y.Sign() == 0 {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		if err != nil {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}

		got, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		v, ok := got.(*big.Int)
		if !ok {
			t.Fatalf("result type = %T, want *big.Int", got)
		}
		if !signedFitsBits(v, 257) {
			t.Fatalf("finite LSHIFTDIV result out of TVM range: %s", v)
		}
	})
}

func FuzzTVMLShiftDivCodeNaNAndOverflowRules(f *testing.F) {
	for _, seed := range []struct {
		d, round         uint8
		x, w, z          int64
		shift            uint8
		nanX, nanW, nanZ bool
	}{
		{d: 0, round: 0, x: 5, w: 3, z: 4, shift: 1},
		{d: 1, round: 1, x: -5, z: 4, shift: 1},
		{d: 2, round: 2, x: 5, z: -4, shift: 1},
		{d: 3, round: 0, x: 5, z: 4, shift: 1},
		{d: 0, round: 0, x: 5, w: 3, z: 0, shift: 1},
		{d: 0, round: 1, x: 5, w: 3, z: 4, shift: 1, nanW: true},
		{d: 1, round: 2, x: 5, z: 4, shift: 1, nanX: true},
		{d: 2, round: 0, x: 5, z: 4, shift: 1, nanZ: true},
	} {
		f.Add(seed.d, seed.round, seed.x, seed.w, seed.z, seed.shift, seed.nanX, seed.nanW, seed.nanZ)
	}

	f.Fuzz(func(t *testing.T, rawD, rawRound uint8, rawX, rawW, rawZ int64, rawShift uint8, nanX, nanW, nanZ bool) {
		d := int(rawD % 4)
		round := int(rawRound % 3)
		st := newMathCoverageState()

		x := big.NewInt(rawX % 1_000_000)
		w := big.NewInt(rawW % 1_000_000)
		z := big.NewInt(rawZ % 1_000_000)
		if rawX%17 == 0 {
			x = tvmEdgeMaxInt()
		}
		if rawX%19 == 0 {
			x = tvmEdgeMinInt()
		}
		if rawW%23 == 0 {
			w = tvmEdgeMaxInt()
		}
		if rawW%29 == 0 {
			w = tvmEdgeMinInt()
		}
		if rawZ%31 == 0 {
			z = big.NewInt(0)
		}

		pushMathCoverageMaybeNaN(t, st, x, nanX)
		if d == 0 {
			pushMathCoverageMaybeNaN(t, st, w, nanW)
		}
		pushMathCoverageMaybeNaN(t, st, z, nanZ)

		err := lshiftDivCodeOp("FUZZ", 0xD0, d, round, fuzzMathSmallImmediate(int64(rawShift))).Interpret(st)
		if nanX || nanZ || d == 0 && nanW || z.Sign() == 0 {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		if err != nil {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}

		wantResults := 2
		if d == 1 || d == 2 {
			wantResults = 1
		}
		for i := 0; i < wantResults; i++ {
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop result %d: %v", i, err)
			}
			v, ok := got.(*big.Int)
			if !ok {
				t.Fatalf("result %d type = %T, want *big.Int", i, got)
			}
			if !signedFitsBits(v, 257) {
				t.Fatalf("finite LSHIFTDIV# result out of TVM range: %s", v)
			}
		}
	})
}
