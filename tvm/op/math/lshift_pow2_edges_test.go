package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestLeftShiftAndPow2ErrorEdges(t *testing.T) {
	t.Run("leftShiftResultNil", func(t *testing.T) {
		if got := leftShiftResult(nil, 7); got != nil {
			t.Fatalf("leftShiftResult nil = %s, want nil", got)
		}
	})

	t.Run("LSHIFTUnderflow", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 7)
		assertMathCoverageVMError(t, LSHIFT().Interpret(st), vmerr.CodeStackUnderflow)
	})

	t.Run("LSHIFTValueType", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageNonInt(t, st)
		pushMathCoverageInts(t, st, 1)
		assertMathCoverageVMError(t, LSHIFT().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("LSHIFTCODEType", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageNonInt(t, st)
		assertMathCoverageVMError(t, LSHIFTCODE(1).Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("LSHIFTCODEText", func(t *testing.T) {
		if got := LSHIFTCODE(3).SerializeText(); got != "3 LSHIFT#" {
			t.Fatalf("LSHIFT# text = %q, want 3 LSHIFT#", got)
		}
	})

	t.Run("LSHIFTCODEShortSuffix", func(t *testing.T) {
		op := LSHIFTCODE(1)
		code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().MustBeginParse()
		if err := op.Deserialize(code); err == nil {
			t.Fatalf("LSHIFT# short suffix deserialize unexpectedly succeeded")
		}
	})

	t.Run("POW2Underflow", func(t *testing.T) {
		st := newMathCoverageState()
		assertMathCoverageVMError(t, POW2().Interpret(st), vmerr.CodeStackUnderflow)
	})

	t.Run("POW2Type", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageNonInt(t, st)
		assertMathCoverageVMError(t, POW2().Interpret(st), vmerr.CodeTypeCheck)
	})
}

func FuzzTVMLeftShiftPow2RangeAndOverflowRules(f *testing.F) {
	for _, seed := range []struct {
		opKind   uint8
		x, shift int64
		nanX     bool
	}{
		{opKind: 0, x: 7, shift: 3},
		{opKind: 0, x: 0, shift: 259},
		{opKind: 0, x: 0, shift: 260},
		{opKind: 0, x: 7, shift: 1024},
		{opKind: 0, x: 7, shift: -1},
		{opKind: 0, x: 7, shift: 52, nanX: true},
		{opKind: 1, x: 7, shift: 3},
		{opKind: 2, shift: 12},
		{opKind: 2, shift: 256},
	} {
		f.Add(seed.opKind, seed.x, seed.shift, seed.nanX)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawShift int64, nanX bool) {
		opKind := rawOp % 3
		st := newMathCoverageState()
		x := big.NewInt(rawX % 1_000_000)
		if rawX%17 == 0 {
			x = tvmEdgeMaxInt()
		}
		if rawX%19 == 0 {
			x = tvmEdgeMinInt()
		}

		shift := rawShift%1200 - 50
		if opKind == 1 {
			shift = int64(fuzzMathSmallImmediate(rawShift))
		}

		if opKind != 2 {
			pushMathCoverageMaybeNaN(t, st, x, nanX)
		}
		if opKind != 1 {
			pushMathCoverageInts(t, st, shift)
		}

		var err error
		switch opKind {
		case 0:
			err = LSHIFT().Interpret(st)
		case 1:
			err = LSHIFTCODE(int(shift)).Interpret(st)
		default:
			err = POW2().Interpret(st)
		}

		if opKind != 1 && (shift < 0 || shift > 1023) {
			assertMathCoverageVMError(t, err, vmerr.CodeRangeCheck)
			return
		}
		if nanX && opKind != 2 {
			if err == nil {
				got, popErr := st.Stack.PopAny()
				if popErr != nil {
					t.Fatalf("pop legacy NaN shift result: %v", popErr)
				}
				if _, ok := got.(*big.Int); !ok {
					t.Fatalf("legacy NaN shift result type = %T, want *big.Int", got)
				}
				return
			}
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		if err != nil {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}

		got, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop left shift/pow2 result: %v", err)
		}
		v, ok := got.(*big.Int)
		if !ok {
			t.Fatalf("result type = %T, want *big.Int", got)
		}
		if !signedFitsBits(v, 257) {
			t.Fatalf("finite left shift/pow2 result out of TVM range: %s", v)
		}
	})
}
