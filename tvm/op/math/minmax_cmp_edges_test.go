package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func pushMathCoverageNonInt(t *testing.T, st *vm.State) {
	t.Helper()
	if err := st.Stack.PushAny(cell.BeginCell().EndCell()); err != nil {
		t.Fatalf("push non-int: %v", err)
	}
}

func pushMathCoverageMaybeNaN(t *testing.T, st *vm.State, v *big.Int, nan bool) {
	t.Helper()
	if nan {
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN: %v", err)
		}
		return
	}
	pushMathCoverageBigInt(t, st, v)
}

func TestMinMaxCompareUnderflowAndTypeEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		push func(t *testing.T, st *vm.State)
		code int64
	}{
		{name: "MINMAX_underflow", op: MINMAX(), code: vmerr.CodeStackUnderflow},
		{name: "MINMAX_type_top", op: MINMAX(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "MINMAX_type_bottom", op: MINMAX(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QABS_underflow", op: QABS(), code: vmerr.CodeStackUnderflow},
		{name: "QABS_type", op: QABS(), code: vmerr.CodeTypeCheck, push: pushMathCoverageNonInt},
		{name: "QCMP_underflow", op: QCMP(), code: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QCMP_type_top", op: QCMP(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QCMP_type_bottom", op: QCMP(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QEQINT_underflow", op: QEQINT(7), code: vmerr.CodeStackUnderflow},
		{name: "QEQINT_type", op: QEQINT(7), code: vmerr.CodeTypeCheck, push: pushMathCoverageNonInt},
		{name: "QSGN_underflow", op: QSGN(), code: vmerr.CodeStackUnderflow},
		{name: "QSGN_type", op: QSGN(), code: vmerr.CodeTypeCheck, push: pushMathCoverageNonInt},
		{name: "ISNAN_underflow", op: ISNAN(), code: vmerr.CodeStackUnderflow},
		{name: "ISNAN_type", op: ISNAN(), code: vmerr.CodeTypeCheck, push: pushMathCoverageNonInt},
		{name: "CHKNAN_underflow", op: CHKNAN(), code: vmerr.CodeStackUnderflow},
		{name: "CHKNAN_type", op: CHKNAN(), code: vmerr.CodeTypeCheck, push: pushMathCoverageNonInt},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			if tt.push != nil {
				tt.push(t, st)
			}
			assertMathCoverageVMError(t, tt.op.Interpret(st), tt.code)
		})
	}
}

func TestMinMaxCompareNaNAndBoundaryEdges(t *testing.T) {
	t.Run("MINMAXSwapsFiniteOperands", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 2, 5)

		if err := MINMAX().Interpret(st); err != nil {
			t.Fatalf("MINMAX interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 5 {
			t.Fatalf("MINMAX max = %d, want 5", got)
		}
		if got := popMathCoverageInt(t, st); got != 2 {
			t.Fatalf("MINMAX min = %d, want 2", got)
		}
	})

	t.Run("MINMAXNaNOperandsOverflow", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			push func(t *testing.T, st *vm.State)
		}{
			{name: "top", push: func(t *testing.T, st *vm.State) {
				pushMathCoverageInts(t, st, 7)
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
			}},
			{name: "bottom", push: func(t *testing.T, st *vm.State) {
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
				pushMathCoverageInts(t, st, 7)
			}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				st := newMathCoverageState()
				tt.push(t, st)
				assertMathCoverageVMError(t, MINMAX().Interpret(st), vmerr.CodeIntOverflow)
			})
		}
	})

	t.Run("QMINMAXRightNaNPropagatesBothResults", func(t *testing.T) {
		st := newMathCoverageState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN: %v", err)
		}
		pushMathCoverageInts(t, st, 5)

		if err := QMINMAX().Interpret(st); err != nil {
			t.Fatalf("QMINMAX interpret: %v", err)
		}
		requireMathStackNaN(t, st)
		requireMathStackNaN(t, st)
	})

	t.Run("QMinAndQMaxNaNResult", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			op   mathInterpretOp
			push func(t *testing.T, st *vm.State)
		}{
			{name: "QMIN_top_nan", op: QMIN(), push: func(t *testing.T, st *vm.State) {
				pushMathCoverageInts(t, st, 7)
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
			}},
			{name: "QMAX_bottom_nan", op: QMAX(), push: func(t *testing.T, st *vm.State) {
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
				pushMathCoverageInts(t, st, 7)
			}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				st := newMathCoverageState()
				tt.push(t, st)
				if err := tt.op.Interpret(st); err != nil {
					t.Fatalf("%s interpret: %v", tt.name, err)
				}
				requireMathStackNaN(t, st)
			})
		}
	})

	t.Run("QABSNaNAndMinIntReturnNaN", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			push func(t *testing.T, st *vm.State)
		}{
			{name: "nan", push: func(t *testing.T, st *vm.State) {
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
			}},
			{name: "min_int", push: func(t *testing.T, st *vm.State) {
				pushMathCoverageBigInt(t, st, tvmEdgeMinInt())
			}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				st := newMathCoverageState()
				tt.push(t, st)
				if err := QABS().Interpret(st); err != nil {
					t.Fatalf("QABS interpret: %v", err)
				}
				requireMathStackNaN(t, st)
			})
		}
	})

	t.Run("NonQuietSignAndCompareNaNOverflow", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			op   mathInterpretOp
			push func(t *testing.T, st *vm.State)
		}{
			{name: "SGN", op: SGN(), push: func(t *testing.T, st *vm.State) {
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
			}},
			{name: "CMP_top", op: CMP(), push: func(t *testing.T, st *vm.State) {
				pushMathCoverageInts(t, st, 1)
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
			}},
			{name: "CMP_bottom", op: CMP(), push: func(t *testing.T, st *vm.State) {
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
				pushMathCoverageInts(t, st, 1)
			}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				st := newMathCoverageState()
				tt.push(t, st)
				assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeIntOverflow)
			})
		}
	})

	t.Run("QuietCompareIntNaNResult", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			op   mathInterpretOp
		}{
			{name: "QEQINT", op: QEQINT(7)},
			{name: "QLESSINT", op: QLESSINT(7)},
			{name: "QGTINT", op: QGTINT(7)},
			{name: "QNEQINT", op: QNEQINT(7)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				st := newMathCoverageState()
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
				if err := tt.op.Interpret(st); err != nil {
					t.Fatalf("%s interpret: %v", tt.name, err)
				}
				requireMathStackNaN(t, st)
			})
		}
	})

	t.Run("ISNANAndCHKNANFinitePaths", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 9)
		if err := ISNAN().Interpret(st); err != nil {
			t.Fatalf("ISNAN interpret: %v", err)
		}
		if got := popMathCoverageBool(t, st); got {
			t.Fatalf("ISNAN finite result = true, want false")
		}

		pushMathCoverageInts(t, st, 11)
		if err := CHKNAN().Interpret(st); err != nil {
			t.Fatalf("CHKNAN interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 11 {
			t.Fatalf("CHKNAN finite result = %d, want 11", got)
		}
	})
}

func TestQuietCompareIntDeserializeRejectsShortSuffix(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0xB7C0, 16).EndCell().MustBeginParse()
	if err := QEQINT(0).Deserialize(code); err == nil {
		t.Fatalf("expected short QEQINT suffix to fail")
	}
}

func FuzzTVMQuietMinMaxCompareNaNRules(f *testing.F) {
	for _, seed := range []struct {
		op         uint8
		x, y       int64
		nanX, nanY bool
	}{
		{op: 0, x: 1, y: 2},
		{op: 1, x: 5, y: -3},
		{op: 2, x: 5, y: -3, nanY: true},
		{op: 3, x: -7},
		{op: 4, nanX: true},
		{op: 5, x: 1, y: 2},
		{op: 11, x: 3, y: 1},
		{op: 12, nanX: true},
		{op: 15, x: -4},
	} {
		f.Add(seed.op, seed.x, seed.y, seed.nanX, seed.nanY)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawY int64, nanX, nanY bool) {
		opKind := rawOp % 16
		st := newMathCoverageState()
		x := big.NewInt(rawX % 1_000_000)
		y := big.NewInt(rawY % 1_000_000)
		if rawX%23 == 0 {
			x = tvmEdgeMinInt()
		}
		if rawX%29 == 0 {
			x = tvmEdgeMaxInt()
		}

		var op mathInterpretOp
		results := 1
		switch opKind {
		case 0:
			op = QMIN()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		case 1:
			op = QMAX()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		case 2:
			op = QMINMAX()
			results = 2
			pushMathCoverageMaybeNaN(t, st, x, nanX)
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		case 3:
			op = QABS()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
		case 4:
			op = QSGN()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
		case 5:
			op = QLESS()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		case 6:
			op = QEQUAL()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		case 7:
			op = QLEQ()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		case 8:
			op = QGREATER()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		case 9:
			op = QNEQ()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		case 10:
			op = QGEQ()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		case 11:
			op = QCMP()
			pushMathCoverageMaybeNaN(t, st, x, nanX)
			pushMathCoverageMaybeNaN(t, st, y, nanY)
		case 12:
			op = QEQINT(int8(rawY))
			pushMathCoverageMaybeNaN(t, st, x, nanX)
		case 13:
			op = QLESSINT(int8(rawY))
			pushMathCoverageMaybeNaN(t, st, x, nanX)
		case 14:
			op = QGTINT(int8(rawY))
			pushMathCoverageMaybeNaN(t, st, x, nanX)
		default:
			op = QNEQINT(int8(rawY))
			pushMathCoverageMaybeNaN(t, st, x, nanX)
		}

		if err := op.Interpret(st); err != nil {
			t.Fatalf("quiet minmax/compare op kind=%d failed: %v", opKind, err)
		}
		for i := 0; i < results; i++ {
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop result %d: %v", i, err)
			}
			switch v := got.(type) {
			case vm.NaN:
			case *big.Int:
				if !signedFitsBits(v, 257) {
					t.Fatalf("finite result out of TVM range: %s", v)
				}
				if opKind >= 4 && (v.Sign() < 0 && v.Cmp(bigIntMinusOne) != 0 || v.Sign() > 0 && v.Cmp(bigIntOne) != 0) {
					t.Fatalf("compare-like result = %s, want -1, 0, or 1", v)
				}
			default:
				t.Fatalf("result type = %T, want int or NaN", got)
			}
		}
	})
}
