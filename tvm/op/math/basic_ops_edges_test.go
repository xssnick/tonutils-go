package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestBasicMinMaxSelectionAndErrorEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		x, y int64
		want int64
	}{
		{name: "MINTopIsMin", op: MIN(), x: 5, y: 2, want: 2},
		{name: "MINBottomIsMin", op: MIN(), x: 2, y: 5, want: 2},
		{name: "MAXTopIsMax", op: MAX(), x: 2, y: 5, want: 5},
		{name: "MAXBottomIsMax", op: MAX(), x: 5, y: 2, want: 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, tt.x, tt.y)
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s interpret: %v", tt.name, err)
			}
			if got := popMathCoverageInt(t, st); got != tt.want {
				t.Fatalf("%s result = %d, want %d", tt.name, got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		push func(t *testing.T, st *vm.State)
		code int64
	}{
		{name: "MINUnderflow", op: MIN(), code: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "MINTypeTop", op: MIN(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "MINTypeBottom", op: MIN(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "MINNaNTop", op: MIN(), code: vmerr.CodeIntOverflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
		}},
		{name: "MINNaNBottom", op: MIN(), code: vmerr.CodeIntOverflow, push: func(t *testing.T, st *vm.State) {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "MAXUnderflow", op: MAX(), code: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "MAXTypeTop", op: MAX(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "MAXTypeBottom", op: MAX(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "MAXNaNTop", op: MAX(), code: vmerr.CodeIntOverflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
		}},
		{name: "MAXNaNBottom", op: MAX(), code: vmerr.CodeIntOverflow, push: func(t *testing.T, st *vm.State) {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			tt.push(t, st)
			assertMathCoverageVMError(t, tt.op.Interpret(st), tt.code)
		})
	}
}

func TestBasicCompareErrorAndNaNEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "LESS", op: LESS()},
		{name: "EQUAL", op: EQUAL()},
		{name: "LEQ", op: LEQ()},
		{name: "GREATER", op: GREATER()},
		{name: "NEQ", op: NEQ()},
		{name: "GEQ", op: GEQ()},
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

func TestBasicLogicAndNotErrorEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "AND", op: AND()},
		{name: "OR", op: OR()},
		{name: "XOR", op: XOR()},
	} {
		t.Run(tt.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			st.GlobalVersion = 13
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(tt.name+"_type_top", func(t *testing.T) {
			st := newMathCoverageState()
			st.GlobalVersion = 13
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"_type_bottom", func(t *testing.T) {
			st := newMathCoverageState()
			st.GlobalVersion = 13
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"_nan_top", func(t *testing.T) {
			st := newMathCoverageState()
			st.GlobalVersion = 13
			pushMathCoverageInts(t, st, 1)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(tt.name+"_nan_bottom", func(t *testing.T) {
			st := newMathCoverageState()
			st.GlobalVersion = 13
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}

	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		push func(t *testing.T, st *vm.State)
		code int64
	}{
		{name: "NOTUnderflow", op: NOT(), code: vmerr.CodeStackUnderflow},
		{name: "NOTType", op: NOT(), code: vmerr.CodeTypeCheck, push: pushMathCoverageNonInt},
		{name: "NOTNaN", op: NOT(), code: vmerr.CodeIntOverflow, push: func(t *testing.T, st *vm.State) {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
		}},
		{name: "QNOTUnderflow", op: QNOT(), code: vmerr.CodeStackUnderflow},
		{name: "QNOTType", op: QNOT(), code: vmerr.CodeTypeCheck, push: pushMathCoverageNonInt},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			if tt.push != nil {
				tt.push(t, st)
			}
			assertMathCoverageVMError(t, tt.op.Interpret(st), tt.code)
		})
	}

	t.Run("QNOTNaN", func(t *testing.T) {
		st := newMathCoverageState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN: %v", err)
		}
		if err := QNOT().Interpret(st); err != nil {
			t.Fatalf("QNOT interpret: %v", err)
		}
		requireMathStackNaN(t, st)
	})
}

func TestBasicIntCompareLargeOperands(t *testing.T) {
	largePositive := new(big.Int).Lsh(big.NewInt(1), 100)
	largeNegative := new(big.Int).Neg(largePositive)

	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		x    *big.Int
		want bool
	}{
		{name: "EQINTLargePositive", op: EQINT(0), x: largePositive, want: false},
		{name: "GTINTLargePositive", op: GTINT(0), x: largePositive, want: true},
		{name: "LESSINTLargeNegative", op: LESSINT(0), x: largeNegative, want: true},
		{name: "NEQINTLargeNegative", op: NEQINT(0), x: largeNegative, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, tt.x)
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s interpret: %v", tt.name, err)
			}
			if got := popMathCoverageBool(t, st); got != tt.want {
				t.Fatalf("%s result = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func FuzzTVMBasicNonQuietOpsNaNAndRange(f *testing.F) {
	for _, seed := range []struct {
		opKind     uint8
		x, y       int64
		nanX, nanY bool
	}{
		{opKind: 0, x: 5, y: 2},
		{opKind: 1, x: 5, y: 2},
		{opKind: 2, x: 1, y: 2},
		{opKind: 7, x: 0x6, y: 0x3},
		{opKind: 10, x: 0x6},
		{opKind: 11, x: 0},
		{opKind: 13, x: -1},
		{opKind: 0, x: 1, nanY: true},
		{opKind: 8, nanX: true, y: 1},
		{opKind: 10, nanX: true},
	} {
		f.Add(seed.opKind, seed.x, seed.y, seed.nanX, seed.nanY)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawY int64, nanX, nanY bool) {
		opKind := rawOp % 15
		st := newMathCoverageState()
		st.GlobalVersion = 13

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
			op = MIN()
		case 1:
			op = MAX()
		case 2:
			op = LESS()
		case 3:
			op = EQUAL()
		case 4:
			op = LEQ()
		case 5:
			op = GREATER()
		case 6:
			op = GEQ()
		case 7:
			op = AND()
		case 8:
			op = OR()
		case 9:
			op = XOR()
		case 10:
			op = NOT()
			binary = false
		case 11:
			op = EQINT(int8(rawY))
			binary = false
		case 12:
			op = GTINT(int8(rawY))
			binary = false
		case 13:
			op = LESSINT(int8(rawY))
			binary = false
		default:
			op = NEQINT(int8(rawY))
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
			t.Fatalf("basic op kind=%d failed: %v", opKind, err)
		}

		got, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		if v, ok := got.(*big.Int); !ok || !signedFitsBits(v, 257) {
			t.Fatalf("result = %T %v, want finite TVM int", got, got)
		}
	})
}
