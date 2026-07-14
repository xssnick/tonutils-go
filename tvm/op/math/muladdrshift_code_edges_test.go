package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestMulAddrShiftCodeModErrorAndBoundaryEdges(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
		text string
	}{
		{name: "MULADDRSHIFT#MOD", op: MULADDRSHIFTCODEMOD(2), text: "2 MULADDRSHIFT#MOD"},
		{name: "MULADDRSHIFTR#MOD", op: MULADDRSHIFTRCODEMOD(2), text: "2 MULADDRSHIFTR#MOD"},
		{name: "MULADDRSHIFTC#MOD", op: MULADDRSHIFTCCODEMOD(2), text: "2 MULADDRSHIFTC#MOD"},
	} {
		t.Run(op.name+"_text", func(t *testing.T) {
			advanced, ok := op.op.(interface {
				SerializeText() string
				MinGlobalVersion() int
			})
			if !ok {
				t.Fatalf("%s does not expose advanced op methods", op.name)
			}
			if got := advanced.MinGlobalVersion(); got != 4 {
				t.Fatalf("%s min version = %d, want 4", op.name, got)
			}
			if got := advanced.SerializeText(); got != op.text {
				t.Fatalf("%s text = %q, want %q", op.name, got, op.text)
			}
		})

		t.Run(op.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(op.name+"_w_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2)
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_y_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_x_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 2, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(op.name+"_w_nan", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push w NaN: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_y_nan", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5)
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
			pushMathCoverageInts(t, st, 2, 1)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})

		t.Run(op.name+"_wide_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageBigInt(t, st, tvmEdgeMaxInt())
			pushMathCoverageInts(t, st, 5, 0)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}

	for _, tt := range []struct {
		name         string
		op           mathInterpretOp
		wantQ, wantR int64
	}{
		{name: "Floor", op: MULADDRSHIFTCODEMOD(2), wantQ: -4, wantR: 2},
		{name: "Round", op: MULADDRSHIFTRCODEMOD(2), wantQ: -3, wantR: -2},
		{name: "Ceil", op: MULADDRSHIFTCCODEMOD(2), wantQ: -3, wantR: -2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, -5, 3, 1)
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s interpret: %v", tt.name, err)
			}
			if got := popMathCoverageInt(t, st); got != tt.wantR {
				t.Fatalf("%s remainder = %d, want %d", tt.name, got, tt.wantR)
			}
			if got := popMathCoverageInt(t, st); got != tt.wantQ {
				t.Fatalf("%s quotient = %d, want %d", tt.name, got, tt.wantQ)
			}
		})
	}
}

func FuzzTVMMulAddrShiftCodeModNaNAndOverflowRules(f *testing.F) {
	for _, seed := range []struct {
		opKind           uint8
		x, y, w          int64
		shift            uint8
		nanX, nanY, nanW bool
	}{
		{opKind: 0, x: 5, y: 2, w: 1, shift: 1},
		{opKind: 1, x: -5, y: 3, w: 1, shift: 2},
		{opKind: 2, x: -5, y: 3, w: 1, shift: 2},
		{opKind: 0, x: 5, y: 2, w: 1, shift: 1, nanX: true},
		{opKind: 1, x: 5, y: 2, w: 1, shift: 1, nanY: true},
		{opKind: 2, x: 5, y: 2, w: 1, shift: 1, nanW: true},
	} {
		f.Add(seed.opKind, seed.x, seed.y, seed.w, seed.shift, seed.nanX, seed.nanY, seed.nanW)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawY, rawW int64, rawShift uint8, nanX, nanY, nanW bool) {
		opKind := rawOp % 3
		st := newMathCoverageState()

		x := big.NewInt(rawX % 1_000_000)
		y := big.NewInt(rawY % 1_000_000)
		w := big.NewInt(rawW % 1_000_000)
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
		if rawW%31 == 0 {
			w = tvmEdgeMaxInt()
		}

		pushMathCoverageMaybeNaN(t, st, x, nanX)
		pushMathCoverageMaybeNaN(t, st, y, nanY)
		pushMathCoverageMaybeNaN(t, st, w, nanW)

		var err error
		switch opKind {
		case 0:
			err = MULADDRSHIFTCODEMOD(fuzzMathSmallImmediate(int64(rawShift))).Interpret(st)
		case 1:
			err = MULADDRSHIFTRCODEMOD(fuzzMathSmallImmediate(int64(rawShift))).Interpret(st)
		default:
			err = MULADDRSHIFTCCODEMOD(fuzzMathSmallImmediate(int64(rawShift))).Interpret(st)
		}

		if nanX || nanY || nanW {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		if err != nil {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}

		for i := 0; i < 2; i++ {
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop result %d: %v", i, err)
			}
			v, ok := got.(*big.Int)
			if !ok {
				t.Fatalf("result %d type = %T, want *big.Int", i, got)
			}
			if !signedFitsBits(v, 257) {
				t.Fatalf("finite MULADDRSHIFT#MOD result out of TVM range: %s", v)
			}
		}
	})
}
