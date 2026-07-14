package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestMulDivAndMulModErrorOrderEdges(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULDIV", op: MULDIV()},
		{name: "MULDIVR", op: MULDIVR()},
		{name: "MULDIVC", op: MULDIVC()},
		{name: "MULMOD", op: MULMOD()},
		{name: "MULMODR", op: MULMODR()},
		{name: "MULMODC", op: MULMODC()},
	} {
		t.Run(op.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, 5, 2)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(op.name+"_z_type", func(t *testing.T) {
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
	}
}

func FuzzTVMMulDivMulModNaNZeroAndOverflowRules(f *testing.F) {
	for _, seed := range []struct {
		opKind            uint8
		x, y, z           int64
		nanX, nanY, nanZ  bool
		forceEdgeOperands bool
	}{
		{opKind: 0, x: 5, y: 2, z: 3},
		{opKind: 1, x: -5, y: 3, z: 4},
		{opKind: 2, x: -5, y: 3, z: 4},
		{opKind: 3, x: 5, y: 2, z: 3},
		{opKind: 4, x: -5, y: 3, z: 4},
		{opKind: 5, x: -5, y: 3, z: 4},
		{opKind: 0, x: 5, y: 2, z: 0},
		{opKind: 3, x: 5, y: 2, z: 0},
		{opKind: 0, x: 5, y: 2, z: 1, nanX: true},
		{opKind: 1, x: 5, y: 2, z: 1, nanY: true},
		{opKind: 4, x: 5, y: 2, z: 1, nanZ: true},
		{opKind: 2, x: 0, y: 2, z: 1, forceEdgeOperands: true},
	} {
		f.Add(seed.opKind, seed.x, seed.y, seed.z, seed.nanX, seed.nanY, seed.nanZ, seed.forceEdgeOperands)
	}

	f.Fuzz(func(t *testing.T, rawOp uint8, rawX, rawY, rawZ int64, nanX, nanY, nanZ, forceEdgeOperands bool) {
		opKind := rawOp % 6
		st := newMathCoverageState()

		x := big.NewInt(rawX % 1_000_000)
		y := big.NewInt(rawY % 1_000_000)
		z := big.NewInt(rawZ % 1_000_000)
		if rawX%17 == 0 || forceEdgeOperands {
			x = tvmEdgeMaxInt()
		}
		if rawX%19 == 0 {
			x = tvmEdgeMinInt()
		}
		if rawY%23 == 0 || forceEdgeOperands {
			y = big.NewInt(2)
		}
		if rawY%29 == 0 {
			y = tvmEdgeMinInt()
		}
		if rawZ%31 == 0 {
			z = big.NewInt(1)
		}

		pushMathCoverageMaybeNaN(t, st, x, nanX)
		pushMathCoverageMaybeNaN(t, st, y, nanY)
		pushMathCoverageMaybeNaN(t, st, z, nanZ)

		var err error
		switch opKind {
		case 0:
			err = MULDIV().Interpret(st)
		case 1:
			err = MULDIVR().Interpret(st)
		case 2:
			err = MULDIVC().Interpret(st)
		case 3:
			err = MULMOD().Interpret(st)
		case 4:
			err = MULMODR().Interpret(st)
		default:
			err = MULMODC().Interpret(st)
		}

		if nanX || nanY || nanZ || z.Sign() == 0 {
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
			t.Fatalf("finite MULDIV/MULMOD family result out of TVM range: %s", v)
		}
	})
}
