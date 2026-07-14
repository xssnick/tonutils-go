package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestFitTinyAndStackErrorEdges(t *testing.T) {
	for _, tt := range []struct {
		name       string
		op         *helpers.AdvancedOP
		prefix     uint64
		prefixBits uint
		quiet      bool
	}{
		{name: "FITS", op: FITS(6), prefix: 0xB4, prefixBits: 8},
		{name: "UFITS", op: UFITS(6), prefix: 0xB5, prefixBits: 8},
		{name: "QFITS", op: QFITS(6), prefix: 0xB7B4, prefixBits: 16, quiet: true},
		{name: "QUFITS", op: QUFITS(6), prefix: 0xB7B5, prefixBits: 16, quiet: true},
	} {
		t.Run(tt.name+"_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"_nan", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			err := tt.op.Interpret(st)
			if !tt.quiet {
				assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
				return
			}
			if err != nil {
				t.Fatalf("%s NaN interpret: %v", tt.name, err)
			}
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop %s NaN result: %v", tt.name, err)
			}
			requireMathNaN(t, got)
		})

		t.Run(tt.name+"_short_suffix", func(t *testing.T) {
			code := cell.BeginCell().MustStoreUInt(tt.prefix, tt.prefixBits).EndCell().MustBeginParse()
			if err := tt.op.Deserialize(code); err == nil {
				t.Fatalf("%s short suffix deserialize unexpectedly succeeded", tt.name)
			}
		})
	}

	for _, tt := range []struct {
		name  string
		op    mathInterpretOp
		quiet bool
	}{
		{name: "FITSX", op: FITSX()},
		{name: "UFITSX", op: UFITSX()},
		{name: "QFITSX", op: QFITSX(), quiet: true},
		{name: "QUFITSX", op: QUFITSX(), quiet: true},
	} {
		t.Run(tt.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 7)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(tt.name+"_width_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 7)
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"_value_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 7)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"_nan_value", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 7)
			err := tt.op.Interpret(st)
			if !tt.quiet {
				assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
				return
			}
			if err != nil {
				t.Fatalf("%s NaN interpret: %v", tt.name, err)
			}
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop %s NaN result: %v", tt.name, err)
			}
			requireMathNaN(t, got)
		})
	}
}

func TestBitSizeTypeEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "BITSIZE", op: BITSIZE()},
		{name: "UBITSIZE", op: UBITSIZE()},
		{name: "QBITSIZE", op: QBITSIZE()},
		{name: "QUBITSIZE", op: QUBITSIZE()},
	} {
		t.Run(tt.name+"_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}
}

func FuzzTVMFitStackRules(f *testing.F) {
	for _, seed := range []struct {
		opKind uint8
		x      int64
		width  int64
		nanX   bool
	}{
		{opKind: 0, x: 63, width: 7},
		{opKind: 1, x: 127, width: 7},
		{opKind: 2, x: 128, width: 7},
		{opKind: 3, x: -1, width: 8},
		{opKind: 0, x: 7, width: -1},
		{opKind: 2, x: 7, width: 1024},
		{opKind: 2, x: 7, width: 7, nanX: true},
	} {
		f.Add(seed.opKind, seed.x, seed.width, seed.nanX)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawWidth int64, nanX bool) {
		opKind := rawOp % 4
		st := newMathCoverageState()

		x := big.NewInt(rawX % 1_000_000)
		if rawX%17 == 0 {
			x = tvmEdgeMaxInt()
		}
		if rawX%19 == 0 {
			x = tvmEdgeMinInt()
		}
		width := rawWidth%1200 - 50

		pushMathCoverageMaybeNaN(t, st, x, nanX)
		if err := st.Stack.PushInt(big.NewInt(width)); err != nil {
			t.Fatalf("push width: %v", err)
		}

		var err error
		unsigned := false
		quiet := false
		switch opKind {
		case 0:
			err = FITSX().Interpret(st)
		case 1:
			unsigned = true
			err = UFITSX().Interpret(st)
		case 2:
			quiet = true
			err = QFITSX().Interpret(st)
		default:
			unsigned = true
			quiet = true
			err = QUFITSX().Interpret(st)
		}

		if width < 0 || width > 1023 {
			assertMathCoverageVMError(t, err, vmerr.CodeRangeCheck)
			return
		}
		if nanX {
			if !quiet {
				assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
				return
			}
			if err != nil {
				t.Fatalf("quiet NaN fit failed: %v", err)
			}
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop quiet NaN fit: %v", err)
			}
			requireMathNaN(t, got)
			return
		}

		fits := signedFitsBits(x, int(width))
		if unsigned {
			fits = unsignedFitsBits(x, int(width))
		}
		if !fits && !quiet {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		if err != nil {
			t.Fatalf("fit stack op failed: %v", err)
		}
		got, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop fit result: %v", err)
		}
		if fits {
			v, ok := got.(*big.Int)
			if !ok {
				t.Fatalf("fit result type = %T, want *big.Int", got)
			}
			if v.Cmp(x) != 0 {
				t.Fatalf("fit result = %s, want %s", v, x)
			}
			return
		}
		requireMathNaN(t, got)
	})
}
