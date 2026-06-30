package math

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func fillQuietCompoundStack(t *testing.T, st *vm.State) {
	t.Helper()

	for {
		err := st.Stack.PushAny(vm.NaN{})
		if err == nil {
			continue
		}
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeStackOverflow {
			t.Fatalf("fill stack error = %v, want stack overflow", err)
		}
		return
	}
}

func TestQuietCompoundLegacyMulShiftNaNResults(t *testing.T) {
	t.Run("QMULRSHIFT v12 NaN product uses legacy right shift", func(t *testing.T) {
		st := newMathCoverageState()
		st.GlobalVersion = 12
		pushMathCoverageInts(t, st, 7)
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN y: %v", err)
		}
		pushMathCoverageInts(t, st, 1)

		if err := qMulShrModFamily(4).Interpret(st); err != nil {
			t.Fatalf("QMULRSHIFT v12 failed: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 0 {
			t.Fatalf("QMULRSHIFT v12 result = %d, want 0", got)
		}
	})

	t.Run("QMULRSHIFTMOD v12 NaN product keeps legacy quotient", func(t *testing.T) {
		st := newMathCoverageState()
		st.GlobalVersion = 12
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN x: %v", err)
		}
		pushMathCoverageInts(t, st, 4, 256)

		if err := qMulShrModFamily(12).Interpret(st); err != nil {
			t.Fatalf("QMULRSHIFTMOD v12 failed: %v", err)
		}
		rem, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop remainder: %v", err)
		}
		requireMathNaN(t, rem)
		if got := popMathCoverageInt(t, st); got != -1 {
			t.Fatalf("QMULRSHIFTMOD v12 quotient = %d, want -1", got)
		}
	})
}

func TestQuietCompoundHelperEdges(t *testing.T) {
	t.Run("qRoundDivZeroDivisor", func(t *testing.T) {
		q, r, err := qRoundDiv(big.NewInt(7), big.NewInt(0), 0)
		if err != nil {
			t.Fatalf("qRoundDiv zero divisor: %v", err)
		}
		if q != nil || r != nil {
			t.Fatalf("qRoundDiv zero divisor q=%v r=%v, want nil nil", q, r)
		}
	})

	t.Run("qRoundDivInvalidSuffix", func(t *testing.T) {
		_, _, err := qRoundDiv(big.NewInt(7), big.NewInt(3), 3)
		assertMathCoverageVMError(t, err, vmerr.CodeInvalidOpcode)
	})

	t.Run("qPushResultNil", func(t *testing.T) {
		st := newMathCoverageState()
		if err := qPushResult(st, nil); err != nil {
			t.Fatalf("qPushResult nil: %v", err)
		}
		got, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop qPushResult nil: %v", err)
		}
		requireMathNaN(t, got)
	})

	t.Run("qPushNaNsStackOverflow", func(t *testing.T) {
		st := newMathCoverageState()
		fillQuietCompoundStack(t, st)
		assertMathCoverageVMError(t, qPushNaNs(st, 1), vmerr.CodeStackOverflow)
	})

	t.Run("qPushSelectedFirstResultOverflow", func(t *testing.T) {
		st := newMathCoverageState()
		fillQuietCompoundStack(t, st)
		assertMathCoverageVMError(t, qPushSelected(st, 3, big.NewInt(1), big.NewInt(2)), vmerr.CodeStackOverflow)
	})

	t.Run("qPushSelectedSecondResultOverflow", func(t *testing.T) {
		st := newMathCoverageState()
		fillQuietCompoundStack(t, st)
		if _, err := st.Stack.PopAny(); err != nil {
			t.Fatalf("pop one stack slot: %v", err)
		}
		assertMathCoverageVMError(t, qPushSelected(st, 3, big.NewInt(1), big.NewInt(2)), vmerr.CodeStackOverflow)
	})

	t.Run("qPushSelectedInvalidSelector", func(t *testing.T) {
		st := newMathCoverageState()
		assertMathCoverageVMError(t, qPushSelected(st, 0, big.NewInt(1), big.NewInt(2)), vmerr.CodeInvalidOpcode)
	})

	t.Run("qCompoundShortSuffix", func(t *testing.T) {
		op := qDivModFamily(0)
		code := cell.BeginCell().MustStoreUInt(0xB7A90, 20).EndCell().MustBeginParse()
		if err := op.Deserialize(code); err == nil {
			t.Fatalf("short q compound suffix deserialize unexpectedly succeeded")
		}
	})
}

func TestQuietCompoundRuntimeErrorAndNaNEdges(t *testing.T) {
	for _, tt := range []struct {
		name          string
		op            mathInterpretOp
		push          func(t *testing.T, st *vm.State)
		errCode       int64
		wantNaNs      int
		globalVersion int
	}{
		{name: "QDIV_underflow", op: qDivModFamily(4), errCode: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
		}},
		{name: "QDIV_y_type", op: qDivModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QDIV_x_type", op: qDivModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 2)
		}},
		{name: "QADDDIVMOD_w_type", op: qDivModFamily(0), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 2)
		}},
		{name: "QADDDIVMOD_nan_w", op: qDivModFamily(0), wantNaNs: 2, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN w: %v", err)
			}
			pushMathCoverageInts(t, st, 2)
		}},
		{name: "QDIV_nan_x", op: qDivModFamily(4), wantNaNs: 1, push: func(t *testing.T, st *vm.State) {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN x: %v", err)
			}
			pushMathCoverageInts(t, st, 2)
		}},
		{name: "QRSHIFT_underflow", op: qShrModFamily(4), errCode: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
		}},
		{name: "QRSHIFT_shift_type", op: qShrModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QRSHIFT_x_type", op: qShrModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QADDRSHIFTMOD_w_type", op: qShrModFamily(0), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QADDRSHIFTMOD_nan_w", op: qShrModFamily(0), wantNaNs: 2, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN w: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QRSHIFT_nan_x", op: qShrModFamily(4), wantNaNs: 1, push: func(t *testing.T, st *vm.State) {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN x: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QMULDIV_underflow", op: qMulDivModFamily(4), errCode: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3)
		}},
		{name: "QMULDIV_z_type", op: qMulDivModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QMULDIV_y_type", op: qMulDivModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 2)
		}},
		{name: "QMULDIV_x_type", op: qMulDivModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 3, 2)
		}},
		{name: "QMULADDDIVMOD_w_type", op: qMulDivModFamily(0), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 2)
		}},
		{name: "QMULDIV_nan_y", op: qMulDivModFamily(4), wantNaNs: 1, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN y: %v", err)
			}
			pushMathCoverageInts(t, st, 2)
		}},
		{name: "QMULRSHIFT_underflow", op: qMulShrModFamily(4), errCode: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3)
		}},
		{name: "QMULADDRSHIFTMOD_underflow", op: qMulShrModFamily(0), errCode: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3, 1)
		}},
		{name: "QMULRSHIFT_shift_type", op: qMulShrModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QMULRSHIFT_bad_shift_range", op: qMulShrModFamily(4), errCode: vmerr.CodeRangeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3, 257)
		}},
		{name: "QMULRSHIFT_y_type", op: qMulShrModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QMULRSHIFT_x_type", op: qMulShrModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 3, 1)
		}},
		{name: "QMULADDRSHIFTMOD_w_type", op: qMulShrModFamily(0), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QMULADDRSHIFTMOD_nan_w", op: qMulShrModFamily(0), wantNaNs: 2, globalVersion: 13, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN w: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QMULRSHIFT_nan_y", op: qMulShrModFamily(4), wantNaNs: 1, globalVersion: 13, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN y: %v", err)
			}
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QLSHIFTDIV_underflow", op: qShlDivModFamily(4), errCode: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3)
		}},
		{name: "QLSHIFTADDDIVMOD_underflow", op: qShlDivModFamily(0), errCode: vmerr.CodeStackUnderflow, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3, 1)
		}},
		{name: "QLSHIFTDIV_shift_type", op: qShlDivModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QLSHIFTDIV_bad_shift_range", op: qShlDivModFamily(4), errCode: vmerr.CodeRangeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7, 3, 257)
		}},
		{name: "QLSHIFTDIV_z_type", op: qShlDivModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QLSHIFTDIV_x_type", op: qShlDivModFamily(4), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 3, 1)
		}},
		{name: "QLSHIFTADDDIVMOD_w_type", op: qShlDivModFamily(0), errCode: vmerr.CodeTypeCheck, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 3, 1)
		}},
		{name: "QLSHIFTADDDIVMOD_nan_w", op: qShlDivModFamily(0), wantNaNs: 2, push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 7)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN w: %v", err)
			}
			pushMathCoverageInts(t, st, 3, 1)
		}},
		{name: "QLSHIFTDIV_nan_x", op: qShlDivModFamily(4), wantNaNs: 1, push: func(t *testing.T, st *vm.State) {
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN x: %v", err)
			}
			pushMathCoverageInts(t, st, 3, 1)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			st.GlobalVersion = 4
			if tt.globalVersion != 0 {
				st.GlobalVersion = tt.globalVersion
			}
			tt.push(t, st)
			err := tt.op.Interpret(st)
			if tt.errCode != 0 {
				assertMathCoverageVMError(t, err, tt.errCode)
				return
			}
			if err != nil {
				t.Fatalf("%s interpret: %v", tt.name, err)
			}
			expectQuietCompoundNaNs(t, st, tt.wantNaNs)
		})
	}
}
