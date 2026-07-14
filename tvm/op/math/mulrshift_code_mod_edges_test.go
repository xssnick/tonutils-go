package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestMulRShiftCodeModErrorAndRoundingEdges(t *testing.T) {
	for _, op := range []struct {
		name   string
		op     mathInterpretOp
		prefix uint64
		text   string
	}{
		{name: "MULRSHIFT#MOD", op: MULRSHIFTCODEMOD(2), prefix: 0xA9BC, text: "2 MULRSHIFT#MOD"},
		{name: "MULRSHIFTR#MOD", op: MULRSHIFTRCODEMOD(2), prefix: 0xA9BD, text: "2 MULRSHIFTR#MOD"},
		{name: "MULRSHIFTC#MOD", op: MULRSHIFTCCODEMOD(2), prefix: 0xA9BE, text: "2 MULRSHIFTC#MOD"},
	} {
		t.Run(op.name+"_text", func(t *testing.T) {
			advanced, ok := op.op.(interface {
				SerializeText() string
				Deserialize(*cell.Slice) error
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

		t.Run(op.name+"_short_suffix", func(t *testing.T) {
			advanced, ok := op.op.(interface {
				Deserialize(*cell.Slice) error
			})
			if !ok {
				t.Fatalf("%s does not expose deserialize", op.name)
			}
			code := cell.BeginCell().MustStoreUInt(op.prefix, 16).EndCell().MustBeginParse()
			if err := advanced.Deserialize(code); err == nil {
				t.Fatalf("%s short suffix deserialize unexpectedly succeeded", op.name)
			}
		})
	}

	for _, tt := range []struct {
		name         string
		op           mathInterpretOp
		wantQ, wantR int64
	}{
		{name: "Floor", op: MULRSHIFTCODEMOD(2), wantQ: -4, wantR: 1},
		{name: "Round", op: MULRSHIFTRCODEMOD(2), wantQ: -4, wantR: 1},
		{name: "Ceil", op: MULRSHIFTCCODEMOD(2), wantQ: -3, wantR: -3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, -5, 3)
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

func FuzzTVMMulRShiftCodeModNaNAndOverflowRules(f *testing.F) {
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
		{opKind: 2, x: 0, y: 3, shift: 1},
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
			err = MULRSHIFTCODEMOD(shift).Interpret(st)
		case 1:
			err = MULRSHIFTRCODEMOD(shift).Interpret(st)
		default:
			err = MULRSHIFTCCODEMOD(shift).Interpret(st)
		}

		if nanX || nanY {
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
				t.Fatalf("finite MULRSHIFT#MOD result out of TVM range: %s", v)
			}
		}
	})
}
