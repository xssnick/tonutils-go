package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestAddrShiftCodeModErrorAndBoundaryEdges(t *testing.T) {
	t.Run("MinVersionAndText", func(t *testing.T) {
		op := ADDRSHIFTCODEMOD(3)
		if got := op.MinGlobalVersion(); got != 4 {
			t.Fatalf("min version = %d, want 4", got)
		}
		if got := op.SerializeText(); got != "3 ADDRSHIFT#MOD" {
			t.Fatalf("text = %q, want %q", got, "3 ADDRSHIFT#MOD")
		}
	})

	for _, tt := range []struct {
		name string
		push func(t *testing.T, st *vm.State)
		code int64
	}{
		{name: "Underflow", code: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "TopType", code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "BottomType", code: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "TopNaN", code: vmerr.CodeIntOverflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
		}},
		{name: "BottomNaN", code: vmerr.CodeIntOverflow, push: func(t *testing.T, st *vm.State) {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			tt.push(t, st)
			assertMathCoverageVMError(t, ADDRSHIFTCODEMOD(1).Interpret(st), tt.code)
		})
	}

	t.Run("QuotientPushOverflow", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 1, 1)
		overflow := new(big.Int).Add(tvmEdgeMaxInt(), bigIntOne)
		op := addrShiftCodeModOp("TEST", helpers.BytesPrefix(0xA9, 0x30), 1, func(_, _ *big.Int) (*big.Int, *big.Int) {
			return overflow, big.NewInt(0)
		})
		assertMathCoverageVMError(t, op.Interpret(st), vmerr.CodeIntOverflow)
	})

	for _, tt := range []struct {
		name         string
		op           mathInterpretOp
		wantQ, wantR int64
	}{
		{name: "Floor", op: ADDRSHIFTCODEMOD(3), wantQ: -1, wantR: 3},
		{name: "Round", op: ADDRSHIFTRCODEMOD(3), wantQ: -1, wantR: 3},
		{name: "Ceil", op: ADDRSHIFTCCODEMOD(3), wantQ: 0, wantR: -5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, -9, 4)
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

func FuzzTVMAddrShiftCodeModNaNAndRangeRules(f *testing.F) {
	for _, seed := range []struct {
		opKind     uint8
		x, w       int64
		shift      uint8
		nanX, nanW bool
	}{
		{opKind: 0, x: 9, w: 4, shift: 3},
		{opKind: 1, x: -9, w: 4, shift: 3},
		{opKind: 2, x: -9, w: 4, shift: 3},
		{opKind: 0, x: 1, w: 2, shift: 1, nanX: true},
		{opKind: 1, x: 1, w: 2, shift: 1, nanW: true},
	} {
		f.Add(seed.opKind, seed.x, seed.w, seed.shift, seed.nanX, seed.nanW)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawW int64, rawShift uint8, nanX, nanW bool) {
		opKind := rawOp % 3
		st := newMathCoverageState()

		x := big.NewInt(rawX % 1_000_000)
		w := big.NewInt(rawW % 1_000_000)
		if rawX%17 == 0 {
			x = tvmEdgeMaxInt()
		}
		if rawX%19 == 0 {
			x = tvmEdgeMinInt()
		}
		if rawW%23 == 0 {
			w = tvmEdgeMaxInt()
		}
		if rawW%29 == 0 {
			w = tvmEdgeMinInt()
		}

		pushMathCoverageMaybeNaN(t, st, x, nanX)
		pushMathCoverageMaybeNaN(t, st, w, nanW)

		var err error
		switch opKind {
		case 0:
			err = ADDRSHIFTCODEMOD(fuzzMathSmallImmediate(int64(rawShift))).Interpret(st)
		case 1:
			err = ADDRSHIFTRCODEMOD(fuzzMathSmallImmediate(int64(rawShift))).Interpret(st)
		default:
			err = ADDRSHIFTCCODEMOD(fuzzMathSmallImmediate(int64(rawShift))).Interpret(st)
		}

		if nanX || nanW {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		if err != nil {
			t.Fatalf("ADDRSHIFT#MOD kind=%d failed: %v", opKind, err)
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
				t.Fatalf("finite ADDRSHIFT#MOD result out of TVM range: %s", v)
			}
		}
	})
}
