package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func quietShiftCodeFromSuffix(t *testing.T, right bool, suffix uint8) mathInterpretOp {
	t.Helper()

	prefix := uint64(0xB7AA)
	op := QLSHIFTCODE(0)
	if right {
		prefix = 0xB7AB
		op = QRSHIFTCODE(0)
	}

	code := cell.BeginCell().
		MustStoreUInt(prefix, 16).
		MustStoreUInt(uint64(suffix), 8).
		EndCell().
		MustBeginParse()
	if err := op.Deserialize(code); err != nil {
		t.Fatalf("deserialize quiet shift code: %v", err)
	}
	return op
}

func TestQuietImmediateShiftCodeFiniteAndBoundaryEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		x    int64
		want int64
	}{
		{name: "QLSHIFT#3", op: QLSHIFTCODE(3), x: 7, want: 56},
		{name: "QRSHIFT#3", op: QRSHIFTCODE(3), x: 56, want: 7},
		{name: "QRSHIFT#256Positive", op: quietShiftCodeFromSuffix(t, true, 255), x: 1, want: 0},
		{name: "QRSHIFT#256Negative", op: quietShiftCodeFromSuffix(t, true, 255), x: -1, want: -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, tt.x)

			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s interpret: %v", tt.name, err)
			}
			if got := popMathCoverageInt(t, st); got != tt.want {
				t.Fatalf("%s result = %d, want %d", tt.name, got, tt.want)
			}
		})
	}

	t.Run("QLSHIFT#256OverflowBecomesNaN", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 1)

		op := quietShiftCodeFromSuffix(t, false, 255)
		if err := op.Interpret(st); err != nil {
			t.Fatalf("QLSHIFT#256 interpret: %v", err)
		}
		requireMathStackNaN(t, st)
	})

	t.Run("SerializeTextReflectsDecodedImmediate", func(t *testing.T) {
		if got := QLSHIFTCODE(3).SerializeText(); got != "3 QLSHIFT#" {
			t.Fatalf("QLSHIFT# text = %q, want 3 QLSHIFT#", got)
		}

		op := quietShiftCodeFromSuffix(t, true, 255)
		if got := op.(*helpers.AdvancedOP).SerializeText(); got != "256 QRSHIFT#" {
			t.Fatalf("QRSHIFT# text = %q, want 256 QRSHIFT#", got)
		}
	})
}

func TestQuietImmediateShiftCodeErrorEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		push func(t *testing.T, st *vm.State)
		code int64
	}{
		{name: "QLSHIFT#Underflow", op: QLSHIFTCODE(1), code: vmerr.CodeStackUnderflow},
		{name: "QRSHIFT#Underflow", op: QRSHIFTCODE(1), code: vmerr.CodeStackUnderflow},
		{name: "QLSHIFT#Type", op: QLSHIFTCODE(1), code: vmerr.CodeTypeCheck, push: pushMathCoverageNonInt},
		{name: "QRSHIFT#Type", op: QRSHIFTCODE(1), code: vmerr.CodeTypeCheck, push: pushMathCoverageNonInt},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			if tt.push != nil {
				tt.push(t, st)
			}
			assertMathCoverageVMError(t, tt.op.Interpret(st), tt.code)
		})
	}

	for _, tt := range []struct {
		name   string
		op     mathInterpretOp
		prefix uint64
	}{
		{name: "QLSHIFT#ShortSuffix", op: QLSHIFTCODE(0), prefix: 0xB7AA},
		{name: "QRSHIFT#ShortSuffix", op: QRSHIFTCODE(0), prefix: 0xB7AB},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code := cell.BeginCell().MustStoreUInt(tt.prefix, 16).EndCell().MustBeginParse()
			if err := tt.op.(*helpers.AdvancedOP).Deserialize(code); err == nil {
				t.Fatalf("expected short suffix to fail")
			}
		})
	}
}

func TestQuietLogicTypeUnderflowAndNaNEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		push func(t *testing.T, st *vm.State)
		code int64
	}{
		{name: "QANDUnderflow", op: QAND(), code: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QANDTypeTop", op: QAND(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QANDTypeBottom", op: QAND(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QORUnderflow", op: QOR(), code: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QORTypeTop", op: QOR(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QORTypeBottom", op: QOR(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QXORUnderflow", op: QXOR(), code: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QXORTypeTop", op: QXOR(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QXORTypeBottom", op: QXOR(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QLSHIFTUnderflow", op: QLSHIFT(), code: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QLSHIFTShiftType", op: QLSHIFT(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QLSHIFTValueType", op: QLSHIFT(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QRSHIFTUnderflow", op: QRSHIFT(), code: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QRSHIFTShiftType", op: QRSHIFT(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QRSHIFTValueType", op: QRSHIFT(), code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QPOW2Underflow", op: QPOW2(), code: vmerr.CodeStackUnderflow},
		{name: "QPOW2Type", op: QPOW2(), code: vmerr.CodeTypeCheck, push: pushMathCoverageNonInt},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			if tt.push != nil {
				tt.push(t, st)
			}
			assertMathCoverageVMError(t, tt.op.Interpret(st), tt.code)
		})
	}

	for _, tt := range []struct {
		name string
		push func(t *testing.T, st *vm.State)
	}{
		{name: "TopNaN", push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
		}},
		{name: "BottomNaN", push: func(t *testing.T, st *vm.State) {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
		}},
	} {
		t.Run("QXOR"+tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			tt.push(t, st)
			if err := QXOR().Interpret(st); err != nil {
				t.Fatalf("QXOR interpret: %v", err)
			}
			requireMathStackNaN(t, st)
		})
	}
}

func FuzzTVMQuietImmediateShiftCodeFiniteRules(f *testing.F) {
	for _, seed := range []struct {
		right  bool
		suffix uint8
		x      int64
	}{
		{right: false, suffix: 0, x: 7},
		{right: false, suffix: 2, x: 7},
		{right: false, suffix: 255, x: 1},
		{right: true, suffix: 2, x: 56},
		{right: true, suffix: 255, x: -1},
		{right: true, suffix: 255, x: 1},
	} {
		f.Add(seed.right, seed.suffix, seed.x)
	}

	f.Fuzz(func(t *testing.T, right bool, suffix uint8, rawX int64) {
		st := newMathCoverageState()
		x := big.NewInt(rawX % 1_000_000)
		if rawX%17 == 0 {
			x = tvmEdgeMaxInt()
		}
		if rawX%19 == 0 {
			x = tvmEdgeMinInt()
		}
		if rawX%23 == 0 {
			x = new(big.Int)
		}
		pushMathCoverageBigInt(t, st, x)

		op := quietShiftCodeFromSuffix(t, right, suffix)
		if err := op.Interpret(st); err != nil {
			t.Fatalf("quiet shift code right=%v suffix=%d failed: %v", right, suffix, err)
		}

		got, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		switch v := got.(type) {
		case vm.NaN:
			if right {
				t.Fatalf("right quiet shift code returned NaN")
			}
		case *big.Int:
			if !signedFitsBits(v, 257) {
				t.Fatalf("finite result out of TVM range: %s", v)
			}
		default:
			t.Fatalf("result type = %T, want int or NaN", got)
		}
	})
}
