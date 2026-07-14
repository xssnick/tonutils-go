package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func requireMathStackNaN(t *testing.T, st *vm.State) {
	t.Helper()
	got, err := st.Stack.PopAny()
	if err != nil {
		t.Fatalf("pop result: %v", err)
	}
	requireMathNaN(t, got)
}

func TestQuietArithmeticOverflowEdges(t *testing.T) {
	maxTVMInt := tvmEdgeMaxInt()
	minTVMInt := tvmEdgeMinInt()

	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		push func(t *testing.T, st *vm.State)
	}{
		{name: "QADD", op: QADD(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QSUB", op: QSUB(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, minTVMInt)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QSUBR", op: QSUBR(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, -2)
		}},
		{name: "QMUL", op: QMUL(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, maxTVMInt)
			pushMathCoverageInts(t, st, 2)
		}},
		{name: "QNEGATE", op: QNEGATE(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, minTVMInt)
		}},
		{name: "QINC", op: QINC(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, maxTVMInt)
		}},
		{name: "QDEC", op: QDEC(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, minTVMInt)
		}},
		{name: "QADDINT", op: QADDINT(1), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, maxTVMInt)
		}},
		{name: "QMULINT", op: QMULINT(2), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageBigInt(t, st, maxTVMInt)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			tt.push(t, st)
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", tt.name, err)
			}
			requireMathStackNaN(t, st)
		})
	}
}

func TestQuietArithmeticNaNAndUnderflowEdges(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
		args int
	}{
		{name: "QADD", op: QADD(), args: 2},
		{name: "QSUB", op: QSUB(), args: 2},
		{name: "QSUBR", op: QSUBR(), args: 2},
		{name: "QMUL", op: QMUL(), args: 2},
		{name: "QNEGATE", op: QNEGATE(), args: 1},
		{name: "QINC", op: QINC(), args: 1},
		{name: "QDEC", op: QDEC(), args: 1},
		{name: "QADDINT", op: QADDINT(-3), args: 1},
		{name: "QMULINT", op: QMULINT(-3), args: 1},
	} {
		t.Run(op.name+"_nan", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			if op.args == 2 {
				pushMathCoverageInts(t, st, 1)
			}
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			requireMathStackNaN(t, st)
		})

		t.Run(op.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			if op.args == 2 {
				pushMathCoverageInts(t, st, 1)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeStackUnderflow)
		})
	}
}

func FuzzTVMQuietArithmeticNaNAndOverflowRules(f *testing.F) {
	seeds := []struct {
		opKind uint8
		x, y   int64
		nan    bool
	}{
		{opKind: 0, x: 1, y: 2},
		{opKind: 1, x: -5, y: 7},
		{opKind: 2, x: 5, y: -7},
		{opKind: 3, x: 9, y: 3},
		{opKind: 4, x: -9},
		{opKind: 5, x: 9},
		{opKind: 6, x: 9},
		{opKind: 7, x: 9},
		{opKind: 8, x: 9},
		{opKind: 0, nan: true},
		{opKind: 4, nan: true},
		{opKind: 7, nan: true},
	}
	for _, seed := range seeds {
		f.Add(seed.opKind, seed.x, seed.y, seed.nan)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawY int64, nan bool) {
		opKind := rawOp % 9
		st := newMathCoverageState()

		if nan {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
		} else {
			x := big.NewInt(rawX % 1_000_000)
			if rawX%17 == 0 {
				x = tvmEdgeMaxInt()
			}
			if rawX%19 == 0 {
				x = tvmEdgeMinInt()
			}
			if err := st.Stack.PushInt(x); err != nil {
				t.Fatalf("push x: %v", err)
			}
		}
		if opKind < 4 {
			pushMathCoverageInts(t, st, rawY%1_000_000)
		}

		var err error
		switch opKind {
		case 0:
			err = QADD().Interpret(st)
		case 1:
			err = QSUB().Interpret(st)
		case 2:
			err = QSUBR().Interpret(st)
		case 3:
			err = QMUL().Interpret(st)
		case 4:
			err = QNEGATE().Interpret(st)
		case 5:
			err = QINC().Interpret(st)
		case 6:
			err = QDEC().Interpret(st)
		case 7:
			err = QADDINT(int8(rawY)).Interpret(st)
		default:
			err = QMULINT(int8(rawY)).Interpret(st)
		}
		if err != nil {
			t.Fatalf("quiet arithmetic op kind=%d failed: %v", opKind, err)
		}

		got, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		switch v := got.(type) {
		case vm.NaN:
		case *big.Int:
			if !signedFitsBits(v, 257) {
				t.Fatalf("quiet arithmetic finite result out of TVM range: %s", v)
			}
		default:
			t.Fatalf("quiet arithmetic result type = %T, want int or NaN", got)
		}
	})
}
