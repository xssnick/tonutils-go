package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestMulModPow2CodeErrorAndRoundingEdges(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
		text string
	}{
		{name: "MULMODPOW2#", op: MULMODPOW2CODE(2), text: "2 MULMODPOW2#"},
		{name: "MULMODPOW2R#", op: MULMODPOW2RCODE(2), text: "2 MULMODPOW2R#"},
		{name: "MULMODPOW2C#", op: MULMODPOW2CCODE(2), text: "2 MULMODPOW2C#"},
	} {
		t.Run(op.name+"_text", func(t *testing.T) {
			advanced, ok := op.op.(interface {
				SerializeText() string
			})
			if !ok {
				t.Fatalf("%s does not expose advanced op methods", op.name)
			}
			if got := advanced.SerializeText(); got != op.text {
				t.Fatalf("%s text = %q, want %q", op.name, got, op.text)
			}
		})

		t.Run(op.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 2)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(op.name+"_y_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 2)
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_x_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 2)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_y_nan", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 2)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push y NaN: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_x_nan", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push x NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 2)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}

	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		want int64
	}{
		{name: "Floor", op: MULMODPOW2CODE(2), want: 1},
		{name: "Round", op: MULMODPOW2RCODE(2), want: 1},
		{name: "Ceil", op: MULMODPOW2CCODE(2), want: -3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, -5, 3)
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s interpret: %v", tt.name, err)
			}
			if got := popMathCoverageInt(t, st); got != tt.want {
				t.Fatalf("%s remainder = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func FuzzTVMMulModPow2CodeNaNAndFiniteRules(f *testing.F) {
	for _, seed := range []struct {
		opKind     uint8
		x, y       int64
		shift      uint8
		nanX, nanY bool
	}{
		{opKind: 0, x: 5, y: 2, shift: 1},
		{opKind: 1, x: -5, y: 3, shift: 2},
		{opKind: 2, x: -5, y: 3, shift: 2},
		{opKind: 0, x: 5, y: 2, shift: 1, nanX: true},
		{opKind: 1, x: 5, y: 2, shift: 1, nanY: true},
	} {
		f.Add(seed.opKind, seed.x, seed.y, seed.shift, seed.nanX, seed.nanY)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawY int64, rawShift uint8, nanX, nanY bool) {
		opKind := rawOp % 3
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
			y = tvmEdgeMaxInt()
		}
		if rawY%29 == 0 {
			y = tvmEdgeMinInt()
		}

		pushMathCoverageMaybeNaN(t, st, x, nanX)
		pushMathCoverageMaybeNaN(t, st, y, nanY)

		shift := fuzzMathSmallImmediate(int64(rawShift))
		var err error
		switch opKind {
		case 0:
			err = MULMODPOW2CODE(shift).Interpret(st)
		case 1:
			err = MULMODPOW2RCODE(shift).Interpret(st)
		default:
			err = MULMODPOW2CCODE(shift).Interpret(st)
		}

		if nanX || nanY {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		if err != nil {
			t.Fatalf("finite MULMODPOW2# family failed: %v", err)
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
			t.Fatalf("finite MULMODPOW2# family result out of TVM range: %s", v)
		}
	})
}
