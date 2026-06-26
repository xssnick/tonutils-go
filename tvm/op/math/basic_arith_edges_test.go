package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestBasicBinaryArithmeticErrorEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "ADD", op: SUM()},
		{name: "MUL", op: MUL()},
		{name: "SUB", op: SUB()},
		{name: "SUBR", op: SUBR()},
	} {
		t.Run(tt.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(tt.name+"_type_top", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"_type_bottom", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"_nan_top", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 1)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(tt.name+"_nan_bottom", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestBasicArithmeticOverflowEdges(t *testing.T) {
	maxTVMInt := tvmEdgeMaxInt()
	minTVMInt := tvmEdgeMinInt()

	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		push func(t *testing.T, st *vm.State)
	}{
		{name: "ADDMaxPlusOne", op: SUM(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "MULMaxTimesTwo", op: MUL(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2)
		}},
		{name: "SUBMinMinusOne", op: SUB(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, minTVMInt)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "SUBRMaxMinusNegativeTwo", op: SUBR(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, -2)
		}},
		{name: "NEGATEMin", op: NEGATE(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, minTVMInt)
		}},
		{name: "INCMax", op: INC(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, maxTVMInt)
		}},
		{name: "DECMin", op: DEC(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, minTVMInt)
		}},
		{name: "ABSMin", op: ABS(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, minTVMInt)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			tt.push(t, st)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestBasicUnaryArithmeticErrorEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "NEGATE", op: NEGATE()},
		{name: "INC", op: INC()},
		{name: "DEC", op: DEC()},
		{name: "ABS", op: ABS()},
	} {
		t.Run(tt.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

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
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func FuzzTVMBasicArithmeticNaNAndOverflowRules(f *testing.F) {
	for _, seed := range []struct {
		opKind     uint8
		x, y       int64
		nanX, nanY bool
	}{
		{opKind: 0, x: 1, y: 2},
		{opKind: 1, x: 3, y: 4},
		{opKind: 2, x: 5, y: 7},
		{opKind: 3, x: 5, y: 7},
		{opKind: 4, x: 7},
		{opKind: 5, x: 7},
		{opKind: 6, x: 7},
		{opKind: 7, x: -7},
		{opKind: 0, nanX: true},
		{opKind: 3, nanY: true},
		{opKind: 7, nanX: true},
	} {
		f.Add(seed.opKind, seed.x, seed.y, seed.nanX, seed.nanY)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawY int64, nanX, nanY bool) {
		opKind := rawOp % 8
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

		var op mathInterpretOp
		binary := true
		switch opKind {
		case 0:
			op = SUM()
		case 1:
			op = MUL()
		case 2:
			op = SUB()
		case 3:
			op = SUBR()
		case 4:
			op = NEGATE()
			binary = false
		case 5:
			op = INC()
			binary = false
		case 6:
			op = DEC()
			binary = false
		default:
			op = ABS()
			binary = false
		}

		pushMathCoverageMaybeNaN(t, st, x, nanX)
		if binary {
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		}

		err := op.Interpret(st)
		if nanX || binary && nanY {
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
			t.Fatalf("result type = %T, want int", got)
		}
		if !signedFitsBits(v, 257) {
			t.Fatalf("finite result out of TVM range: %s", v)
		}
	})
}
