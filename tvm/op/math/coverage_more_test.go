package math

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func newMathCoverageState() *vm.State {
	return &vm.State{
		Stack: vm.NewStack(),
		Gas:   vm.NewGas(),
	}
}

func pushMathCoverageInts(t *testing.T, st *vm.State, vals ...int64) {
	t.Helper()
	for _, v := range vals {
		if err := st.Stack.PushInt(big.NewInt(v)); err != nil {
			t.Fatalf("push int %d: %v", v, err)
		}
	}
}

func popMathCoverageInt(t *testing.T, st *vm.State) int64 {
	t.Helper()
	v, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop int: %v", err)
	}
	return v.Int64()
}

func popMathCoverageBool(t *testing.T, st *vm.State) bool {
	t.Helper()
	v, err := st.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop bool: %v", err)
	}
	return v
}

func popMathCoverageMaybeInt(t *testing.T, st *vm.State) *big.Int {
	t.Helper()
	v, err := st.Stack.PopInt()
	if err != nil {
		t.Fatalf("pop maybe int: %v", err)
	}
	return v
}

func assertMathCoverageVMError(t *testing.T, err error, code int64) {
	t.Helper()
	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) {
		t.Fatalf("expected VMError %d, got %T (%v)", code, err, err)
	}
	if vmErr.Code != code {
		t.Fatalf("vm error code = %d, want %d", vmErr.Code, code)
	}
}

func mustRoundTripMathAdvanced(t *testing.T, src *helpers.AdvancedOP, dst *helpers.AdvancedOP, wantText string) {
	t.Helper()
	if err := dst.Deserialize(src.Serialize().EndCell().BeginParse()); err != nil {
		t.Fatalf("deserialize advanced op: %v", err)
	}
	if got := dst.SerializeText(); got != wantText {
		t.Fatalf("serialize text = %q, want %q", got, wantText)
	}
}

func TestMathBasicWrappers(t *testing.T) {
	tests := []struct {
		name string
		op   vm.OP
		push []int64
		int  *int64
		bool *bool
	}{
		{name: "SUM", op: SUM(), push: []int64{2, 3}, int: func() *int64 { v := int64(5); return &v }()},
		{name: "MUL", op: MUL(), push: []int64{4, -2}, int: func() *int64 { v := int64(-8); return &v }()},
		{name: "AND", op: AND(), push: []int64{6, 3}, int: func() *int64 { v := int64(2); return &v }()},
		{name: "MIN", op: MIN(), push: []int64{7, 4}, int: func() *int64 { v := int64(4); return &v }()},
		{name: "MAX", op: MAX(), push: []int64{7, 4}, int: func() *int64 { v := int64(7); return &v }()},
		{name: "EQUAL", op: EQUAL(), push: []int64{9, 9}, bool: func() *bool { v := true; return &v }()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, tt.push...)
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s interpret: %v", tt.name, err)
			}

			if tt.int != nil {
				if got := popMathCoverageInt(t, st); got != *tt.int {
					t.Fatalf("%s result = %d, want %d", tt.name, got, *tt.int)
				}
				return
			}
			if got := popMathCoverageBool(t, st); got != *tt.bool {
				t.Fatalf("%s result = %t, want %t", tt.name, got, *tt.bool)
			}
		})
	}
}

func TestMathInitRegistrationsInstantiateOps(t *testing.T) {
	if len(vm.List) == 0 {
		t.Fatal("vm.List should contain registered math ops")
	}

	instantiated := 0
	for _, getter := range vm.List {
		if getter == nil {
			continue
		}
		if op := getter(); op != nil {
			instantiated++
		}
	}
	if instantiated == 0 {
		t.Fatal("expected at least one op getter to instantiate an op")
	}
}

func TestMathImmediateAndAdvancedAliases(t *testing.T) {
	t.Run("BytePlusOneImmediateRoundTrip", func(t *testing.T) {
		get, serialize, deserialize := newBytePlusOneImmediate(3)
		if got := get(); got != 3 {
			t.Fatalf("initial immediate = %d, want 3", got)
		}

		encoded, err := serialize().EndCell().BeginParse().LoadUInt(8)
		if err != nil {
			t.Fatalf("load encoded immediate: %v", err)
		}
		if encoded != 2 {
			t.Fatalf("encoded immediate = %d, want 2", encoded)
		}

		if err := deserialize(vmCellWithByte(t, 9)); err != nil {
			t.Fatalf("deserialize immediate: %v", err)
		}
		if got := get(); got != 10 {
			t.Fatalf("decoded immediate = %d, want 10", got)
		}
	})

	t.Run("AdvancedAliasRoundTripAndInterpret", func(t *testing.T) {
		mustRoundTripMathAdvanced(t, ADDINT(-3), ADDCONST(0), "ADDINT -3")
		mustRoundTripMathAdvanced(t, MULINT(4), MULCONST(0), "MULINT 4")
		mustRoundTripMathAdvanced(t, EQINT(-5), EQINT(0), "-5 EQINT")
		mustRoundTripMathAdvanced(t, GTINT(6), GTINT(0), "6 GTINT")
		mustRoundTripMathAdvanced(t, LESSINT(-7), LESSINT(0), "-7 LESSINT")
		mustRoundTripMathAdvanced(t, NEQINT(8), NEQINT(0), "8 NEQINT")

		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 10)
		if err := ADDINT(-3).Interpret(st); err != nil {
			t.Fatalf("ADDINT interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 7 {
			t.Fatalf("ADDINT result = %d, want 7", got)
		}

		pushMathCoverageInts(t, st, -2)
		if err := MULINT(4).Interpret(st); err != nil {
			t.Fatalf("MULINT interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != -8 {
			t.Fatalf("MULINT result = %d, want -8", got)
		}
	})
}

func TestMathBitHelpers(t *testing.T) {
	if signedFitsBits(nil, 3) {
		t.Fatal("nil should not fit signed bits")
	}
	if !signedFitsBits(big.NewInt(0), 0) {
		t.Fatal("zero should fit signed bits")
	}
	if signedFitsBits(big.NewInt(1), 0) {
		t.Fatal("positive value should not fit non-positive signed width")
	}
	if !signedFitsBits(big.NewInt(-4), 3) {
		t.Fatal("-4 should fit in 3 signed bits")
	}
	if signedFitsBits(big.NewInt(4), 3) {
		t.Fatal("4 should not fit in 3 signed bits")
	}

	if unsignedFitsBits(nil, 2) {
		t.Fatal("nil should not fit unsigned bits")
	}
	if !unsignedFitsBits(big.NewInt(0), -1) {
		t.Fatal("zero should fit any unsigned width")
	}
	if unsignedFitsBits(big.NewInt(-1), 4) {
		t.Fatal("negative value should not fit unsigned bits")
	}
	if !unsignedFitsBits(big.NewInt(7), 3) {
		t.Fatal("7 should fit in 3 unsigned bits")
	}
	if unsignedFitsBits(big.NewInt(8), 3) {
		t.Fatal("8 should not fit in 3 unsigned bits")
	}

	if got := signedBitSize(nil); got != bitSizeInvalid {
		t.Fatalf("signed bit size(nil) = %d, want invalid", got)
	}
	if got := signedBitSize(big.NewInt(0)); got != 0 {
		t.Fatalf("signed bit size(0) = %d, want 0", got)
	}
	if got := signedBitSize(big.NewInt(7)); got != 4 {
		t.Fatalf("signed bit size(7) = %d, want 4", got)
	}
	if got := signedBitSize(big.NewInt(-8)); got != 4 {
		t.Fatalf("signed bit size(-8) = %d, want 4", got)
	}

	if got := unsignedBitSize(nil); got != bitSizeInvalid {
		t.Fatalf("unsigned bit size(nil) = %d, want invalid", got)
	}
	if got := unsignedBitSize(big.NewInt(-1)); got != bitSizeInvalid {
		t.Fatalf("unsigned bit size(-1) = %d, want invalid", got)
	}
	if got := unsignedBitSize(big.NewInt(0)); got != 0 {
		t.Fatalf("unsigned bit size(0) = %d, want 0", got)
	}
	if got := unsignedBitSize(big.NewInt(8)); got != 4 {
		t.Fatalf("unsigned bit size(8) = %d, want 4", got)
	}
}

func TestMathFitsAndBitSizeVariants(t *testing.T) {
	t.Run("TinyFitsVariants", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 1)
		if err := FITS(1).Interpret(st); err != nil {
			t.Fatalf("FITS interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 1 {
			t.Fatalf("FITS result = %d, want 1", got)
		}

		pushMathCoverageInts(t, st, 7)
		if err := UFITS(2).Interpret(st); err != nil {
			t.Fatalf("UFITS interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 7 {
			t.Fatalf("UFITS result = %d, want 7", got)
		}

		pushMathCoverageInts(t, st, 2)
		if err := QFITS(1).Interpret(st); err != nil {
			t.Fatalf("QFITS interpret: %v", err)
		}
		if got := popMathCoverageMaybeInt(t, st); got != nil {
			t.Fatalf("QFITS result = %v, want NaN", got)
		}

		pushMathCoverageInts(t, st, -1)
		if err := QUFITS(1).Interpret(st); err != nil {
			t.Fatalf("QUFITS interpret: %v", err)
		}
		if got := popMathCoverageMaybeInt(t, st); got != nil {
			t.Fatalf("QUFITS result = %v, want NaN", got)
		}
	})

	t.Run("StackFitsAndBitSizeVariants", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 7, 3)
		if err := UFITSX().Interpret(st); err != nil {
			t.Fatalf("UFITSX interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 7 {
			t.Fatalf("UFITSX result = %d, want 7", got)
		}

		pushMathCoverageInts(t, st, -1, 3)
		if err := QUFITSX().Interpret(st); err != nil {
			t.Fatalf("QUFITSX interpret: %v", err)
		}
		if got := popMathCoverageMaybeInt(t, st); got != nil {
			t.Fatalf("QUFITSX result = %v, want NaN", got)
		}

		pushMathCoverageInts(t, st, 7)
		if err := QBITSIZE().Interpret(st); err != nil {
			t.Fatalf("QBITSIZE interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 4 {
			t.Fatalf("QBITSIZE result = %d, want 4", got)
		}

		pushMathCoverageInts(t, st, -1)
		if err := QUBITSIZE().Interpret(st); err != nil {
			t.Fatalf("QUBITSIZE interpret: %v", err)
		}
		if got := popMathCoverageMaybeInt(t, st); got != nil {
			t.Fatalf("QUBITSIZE result = %v, want NaN", got)
		}

		pushMathCoverageInts(t, st, -1)
		err := UBITSIZE().Interpret(st)
		assertMathCoverageVMError(t, err, vmerr.CodeRangeCheck)
	})
}

func TestMathQuietMinMaxCompareAndSignBranches(t *testing.T) {
	t.Run("QMINMAXPropagatesNaNToBothResults", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 5)
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN: %v", err)
		}

		if err := QMINMAX().Interpret(st); err != nil {
			t.Fatalf("QMINMAX interpret: %v", err)
		}
		if got := popMathCoverageMaybeInt(t, st); got != nil {
			t.Fatalf("QMINMAX max result = %v, want NaN", got)
		}
		if got := popMathCoverageMaybeInt(t, st); got != nil {
			t.Fatalf("QMINMAX min result = %v, want NaN", got)
		}
	})

	t.Run("SignAndQuietCompareWrappers", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, -3)
		if err := SGN().Interpret(st); err != nil {
			t.Fatalf("SGN interpret: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != -1 {
			t.Fatalf("SGN result = %d, want -1", got)
		}

		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN for QSGN: %v", err)
		}
		if err := QSGN().Interpret(st); err != nil {
			t.Fatalf("QSGN interpret: %v", err)
		}
		if got := popMathCoverageMaybeInt(t, st); got != nil {
			t.Fatalf("QSGN result = %v, want NaN", got)
		}

		pushMathCoverageInts(t, st, 1)
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN for QCMP: %v", err)
		}
		if err := QCMP().Interpret(st); err != nil {
			t.Fatalf("QCMP interpret: %v", err)
		}
		if got := popMathCoverageMaybeInt(t, st); got != nil {
			t.Fatalf("QCMP result = %v, want NaN", got)
		}
	})
}

func vmCellWithByte(t *testing.T, v uint64) *cell.Slice {
	t.Helper()
	return cell.BeginCell().MustStoreUInt(v, 8).EndCell().BeginParse()
}

func TestMathAdditionalWrappersAndQuietOps(t *testing.T) {
	t.Run("PlainWrappers", func(t *testing.T) {
		tests := []struct {
			name string
			op   vm.OP
			push []int64
			int  *int64
			bool *bool
		}{
			{name: "NEGATE", op: NEGATE(), push: []int64{5}, int: func() *int64 { v := int64(-5); return &v }()},
			{name: "NEQ", op: NEQ(), push: []int64{5, 4}, bool: func() *bool { v := true; return &v }()},
			{name: "OR", op: OR(), push: []int64{6, 3}, int: func() *int64 { v := int64(7); return &v }()},
			{name: "XOR", op: XOR(), push: []int64{6, 3}, int: func() *int64 { v := int64(5); return &v }()},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				st := newMathCoverageState()
				pushMathCoverageInts(t, st, tt.push...)
				if err := tt.op.Interpret(st); err != nil {
					t.Fatalf("%s interpret: %v", tt.name, err)
				}

				if tt.int != nil {
					if got := popMathCoverageInt(t, st); got != *tt.int {
						t.Fatalf("%s result = %d, want %d", tt.name, got, *tt.int)
					}
				} else if got := popMathCoverageBool(t, st); got != *tt.bool {
					t.Fatalf("%s result = %t, want %t", tt.name, got, *tt.bool)
				}
			})
		}
	})

	t.Run("QuietArithmeticAndLogic", func(t *testing.T) {
		tests := []struct {
			name string
			op   vm.OP
			push []int64
			want int64
		}{
			{name: "QSUB", op: QSUB(), push: []int64{7, 2}, want: 5},
			{name: "QSUBR", op: QSUBR(), push: []int64{7, 2}, want: -5},
			{name: "QMUL", op: QMUL(), push: []int64{3, 4}, want: 12},
			{name: "QOR", op: QOR(), push: []int64{6, 3}, want: 7},
			{name: "QXOR", op: QXOR(), push: []int64{6, 3}, want: 5},
			{name: "QLSHIFT", op: QLSHIFT(), push: []int64{3, 2}, want: 12},
			{name: "QRSHIFT", op: QRSHIFT(), push: []int64{12, 2}, want: 3},
			{name: "QNOT", op: QNOT(), push: []int64{15}, want: -16},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				st := newMathCoverageState()
				pushMathCoverageInts(t, st, tt.push...)
				if err := tt.op.Interpret(st); err != nil {
					t.Fatalf("%s interpret: %v", tt.name, err)
				}
				if got := popMathCoverageInt(t, st); got != tt.want {
					t.Fatalf("%s result = %d, want %d", tt.name, got, tt.want)
				}
			})
		}

		st := newMathCoverageState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN for quiet shift: %v", err)
		}
		pushMathCoverageInts(t, st, 2)
		if err := QLSHIFT().Interpret(st); err != nil {
			t.Fatalf("QLSHIFT NaN interpret: %v", err)
		}
		if got := popMathCoverageMaybeInt(t, st); got != nil {
			t.Fatalf("QLSHIFT NaN result = %v, want NaN", got)
		}
	})
}

func TestMathAdvancedRoundTripsForQuietAndConstOps(t *testing.T) {
	mustRoundTripMathAdvanced(t, QADDINT(-3), QADDINT(0), "QADDINT -3")
	mustRoundTripMathAdvanced(t, QMULINT(4), QMULINT(0), "QMULINT 4")
	mustRoundTripMathAdvanced(t, QEQINT(-5), QEQINT(0), "QEQINT -5")
	mustRoundTripMathAdvanced(t, QLESSINT(6), QLESSINT(0), "QLESSINT 6")
	mustRoundTripMathAdvanced(t, QGTINT(-7), QGTINT(0), "QGTINT -7")
	mustRoundTripMathAdvanced(t, QNEQINT(8), QNEQINT(0), "QNEQINT 8")
	mustRoundTripMathAdvanced(t, FITS(3), FITS(0), "FITS 4")
	mustRoundTripMathAdvanced(t, QFITS(4), QFITS(0), "QFITS 5")
	mustRoundTripMathAdvanced(t, PUSHPOW2(4), PUSHPOW2(0), "PUSHPOW2 5")
	mustRoundTripMathAdvanced(t, PUSHPOW2DEC(4), PUSHPOW2DEC(0), "PUSHPOW2DEC 5")
	mustRoundTripMathAdvanced(t, PUSHNEGPOW2(4), PUSHNEGPOW2(0), "PUSHNEGPOW2 5")

	st := newMathCoverageState()
	pushMathCoverageInts(t, st, 10)
	if err := QADDINT(-3).Interpret(st); err != nil {
		t.Fatalf("QADDINT interpret: %v", err)
	}
	if got := popMathCoverageInt(t, st); got != 7 {
		t.Fatalf("QADDINT result = %d, want 7", got)
	}

	pushMathCoverageInts(t, st, -2)
	if err := QMULINT(4).Interpret(st); err != nil {
		t.Fatalf("QMULINT interpret: %v", err)
	}
	if got := popMathCoverageInt(t, st); got != -8 {
		t.Fatalf("QMULINT result = %d, want -8", got)
	}
}
