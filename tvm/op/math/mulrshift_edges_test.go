package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestMulRShiftDynamicErrorEdges(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULRSHIFT", op: MULRSHIFT()},
		{name: "MULRSHIFTR", op: MULRSHIFTR()},
		{name: "MULRSHIFTC", op: MULRSHIFTC()},
	} {
		t.Run(op.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 1, 2)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(op.name+"_shift_range", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 2, 3, 257)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})

		t.Run(op.name+"_shift_nan", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 2, 3)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push shift NaN: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})

		t.Run(op.name+"_shift_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 2, 3)
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_y_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 2)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_x_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 3, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_y_nan", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 2)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push y NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_x_nan", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push x NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 3, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestMulRShiftDynamicWideAndRoundingEdges(t *testing.T) {
	t.Run("MULRSHIFTWideShiftOneFits", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageBigInt(t, st, tvmEdgeMaxInt())
		pushMathCoverageInts(t, st, 2, 1)

		if err := MULRSHIFT().Interpret(st); err != nil {
			t.Fatalf("MULRSHIFT interpret: %v", err)
		}
		if got := popMathCoverageBigInt(t, st); got.Cmp(tvmEdgeMaxInt()) != 0 {
			t.Fatalf("MULRSHIFT result = %s, want max TVM int", got)
		}
	})

	t.Run("MULRSHIFTWideShiftZeroOverflows", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageBigInt(t, st, tvmEdgeMaxInt())
		pushMathCoverageInts(t, st, 2, 0)
		assertMathCoverageVMError(t, MULRSHIFT().Interpret(st), vmerr.CodeIntOverflow)
	})

	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		want int64
	}{
		{name: "MULRSHIFTFloor", op: MULRSHIFT(), want: 3},
		{name: "MULRSHIFTRRound", op: MULRSHIFTR(), want: 4},
		{name: "MULRSHIFTCCeil", op: MULRSHIFTC(), want: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 3, 2)
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s interpret: %v", tt.name, err)
			}
			if got := popMathCoverageInt(t, st); got != tt.want {
				t.Fatalf("%s result = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestMulRShiftCodeErrorEdges(t *testing.T) {
	for _, op := range []struct {
		name   string
		op     *helpers.AdvancedOP
		prefix uint64
		text   string
	}{
		{name: "MULRSHIFT#", op: MULRSHIFTCODE(2), prefix: 0xA9B4, text: "2 MULRSHIFT#"},
		{name: "MULRSHIFTR#", op: MULRSHIFTRCODE(2), prefix: 0xA9B5, text: "2 MULRSHIFTR#"},
		{name: "MULRSHIFTC#", op: MULRSHIFTCCODE(2), prefix: 0xA9B6, text: "2 MULRSHIFTC#"},
	} {
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

		t.Run(op.name+"_serialize_text", func(t *testing.T) {
			if got := op.op.SerializeText(); got != op.text {
				t.Fatalf("%s text = %q, want %q", op.name, got, op.text)
			}
		})

		t.Run(op.name+"_short_suffix", func(t *testing.T) {
			code := cell.BeginCell().MustStoreUInt(op.prefix, 16).EndCell().MustBeginParse()
			if err := op.op.Deserialize(code); err == nil {
				t.Fatalf("%s short suffix deserialize should fail", op.name)
			}
		})
	}
}

func TestMulRShiftCodeWideAndRoundingEdges(t *testing.T) {
	t.Run("MULRSHIFTCodeWideShiftOneFits", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageBigInt(t, st, tvmEdgeMaxInt())
		pushMathCoverageInts(t, st, 2)

		if err := MULRSHIFTCODE(1).Interpret(st); err != nil {
			t.Fatalf("MULRSHIFT# interpret: %v", err)
		}
		if got := popMathCoverageBigInt(t, st); got.Cmp(tvmEdgeMaxInt()) != 0 {
			t.Fatalf("MULRSHIFT# result = %s, want max TVM int", got)
		}
	})

	t.Run("MULRSHIFTCodeWideShiftOneOverflows", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageBigInt(t, st, tvmEdgeMaxInt())
		pushMathCoverageInts(t, st, 3)
		assertMathCoverageVMError(t, MULRSHIFTCODE(1).Interpret(st), vmerr.CodeIntOverflow)
	})

	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		want int64
	}{
		{name: "MULRSHIFT#Floor", op: MULRSHIFTCODE(2), want: 3},
		{name: "MULRSHIFTR#Round", op: MULRSHIFTRCODE(2), want: 4},
		{name: "MULRSHIFTC#Ceil", op: MULRSHIFTCCODE(2), want: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 3)
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s interpret: %v", tt.name, err)
			}
			if got := popMathCoverageInt(t, st); got != tt.want {
				t.Fatalf("%s result = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func FuzzTVMMulRShiftNaNRangeAndOverflowRules(f *testing.F) {
	for _, seed := range []struct {
		opKind               uint8
		x, y                 int64
		shift                uint16
		nanX, nanY, nanShift bool
	}{
		{opKind: 0, x: 5, y: 3, shift: 2},
		{opKind: 1, x: -5, y: 3, shift: 2},
		{opKind: 2, x: 5, y: -3, shift: 2},
		{opKind: 3, x: 5, y: 3, shift: 2},
		{opKind: 4, x: -5, y: 3, shift: 2},
		{opKind: 5, x: 5, y: -3, shift: 2},
		{opKind: 0, x: 1, y: 2, shift: 257},
		{opKind: 0, x: 1, y: 2, nanShift: true},
		{opKind: 1, x: 1, y: 2, nanX: true},
		{opKind: 4, x: 1, y: 2, nanY: true},
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
		if rawY%23 == 0 {
			y = tvmEdgeMaxInt()
		}
		if rawY%29 == 0 {
			y = tvmEdgeMinInt()
		}

		dynamic := opKind < 3
		pushMathCoverageMaybeNaN(t, st, x, nanX)
		pushMathCoverageMaybeNaN(t, st, y, nanY)
		if dynamic {
			if nanShift {
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push shift NaN: %v", err)
				}
			} else if err := st.Stack.PushInt(big.NewInt(int64(rawShift))); err != nil {
				t.Fatalf("push shift: %v", err)
			}
		}

		var err error
		switch opKind {
		case 0:
			err = MULRSHIFT().Interpret(st)
		case 1:
			err = MULRSHIFTR().Interpret(st)
		case 2:
			err = MULRSHIFTC().Interpret(st)
		case 3:
			err = MULRSHIFTCODE(fuzzMathSmallImmediate(int64(rawShift))).Interpret(st)
		case 4:
			err = MULRSHIFTRCODE(fuzzMathSmallImmediate(int64(rawShift))).Interpret(st)
		default:
			err = MULRSHIFTCCODE(fuzzMathSmallImmediate(int64(rawShift))).Interpret(st)
		}

		if dynamic && (nanShift || rawShift > 256) {
			assertMathCoverageVMError(t, err, vmerr.CodeRangeCheck)
			return
		}
		if nanX || nanY {
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
			t.Fatalf("finite MULRSHIFT result out of TVM range: %s", v)
		}
	})
}
