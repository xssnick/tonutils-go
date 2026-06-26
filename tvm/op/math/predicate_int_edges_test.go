package math

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestSignPredicateErrorEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "ISZERO", op: ISZERO()},
		{name: "ISPOS", op: ISPOS()},
		{name: "ISNEG", op: ISNEG()},
		{name: "ISNNEG", op: ISNNEG()},
		{name: "ISNPOS", op: ISNPOS()},
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

func TestImmediateCompareIntErrorAndDecodeEdges(t *testing.T) {
	for _, tt := range []struct {
		name   string
		op     *helpers.AdvancedOP
		prefix uint64
	}{
		{name: "EQINT", op: EQINT(7), prefix: 0xC0},
		{name: "LESSINT", op: LESSINT(7), prefix: 0xC1},
		{name: "GTINT", op: GTINT(7), prefix: 0xC2},
		{name: "NEQINT", op: NEQINT(7), prefix: 0xC3},
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

		t.Run(tt.name+"_short_suffix", func(t *testing.T) {
			code := cell.BeginCell().MustStoreUInt(tt.prefix, 8).EndCell().MustBeginParse()
			if err := tt.op.Deserialize(code); err == nil {
				t.Fatalf("expected short %s suffix to fail", tt.name)
			}
		})
	}
}

func FuzzTVMSignPredicatesAndImmediateCompareInts(f *testing.F) {
	for _, seed := range []struct {
		opKind uint8
		x      int64
		nan    bool
	}{
		{opKind: 0, x: 0},
		{opKind: 1, x: 7},
		{opKind: 2, x: -7},
		{opKind: 3, x: 0},
		{opKind: 4, x: 0},
		{opKind: 5, x: 7},
		{opKind: 6, x: -7},
		{opKind: 7, x: 7},
		{opKind: 8, x: -7},
		{opKind: 0, nan: true},
		{opKind: 5, nan: true},
	} {
		f.Add(seed.opKind, seed.x, seed.nan)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, x int64, nan bool) {
		opKind := rawOp % 9
		st := newMathCoverageState()
		if nan {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
		} else {
			pushMathCoverageInts(t, st, x%1_000_000)
		}

		var op mathInterpretOp
		switch opKind {
		case 0:
			op = ISZERO()
		case 1:
			op = ISPOS()
		case 2:
			op = ISNEG()
		case 3:
			op = ISNNEG()
		case 4:
			op = ISNPOS()
		case 5:
			op = EQINT(int8(x >> 8))
		case 6:
			op = LESSINT(int8(x >> 8))
		case 7:
			op = GTINT(int8(x >> 8))
		default:
			op = NEQINT(int8(x >> 8))
		}

		err := op.Interpret(st)
		if nan {
			assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
			return
		}
		if err != nil {
			t.Fatalf("predicate/int compare kind=%d failed: %v", opKind, err)
		}
		if _, err = st.Stack.PopBool(); err != nil {
			t.Fatalf("pop bool result: %v", err)
		}
	})
}
