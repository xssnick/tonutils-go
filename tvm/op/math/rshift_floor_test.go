package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestRshiftFloorBoundariesAndErrors(t *testing.T) {
	t.Run("DynamicBoundaries", func(t *testing.T) {
		tests := []struct {
			name  string
			x     int64
			shift int64
			want  int64
		}{
			{name: "zero_shift", x: -7, shift: 0, want: -7},
			{name: "negative_floor", x: -7, shift: 1, want: -4},
			{name: "positive_256", x: 7, shift: 256, want: 0},
			{name: "negative_256", x: -1, shift: 256, want: -1},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				st := newMathCoverageState()
				pushMathCoverageInts(t, st, tt.x, tt.shift)
				if err := RSHIFTFLOOR().Interpret(st); err != nil {
					t.Fatalf("RSHIFTFLOOR interpret: %v", err)
				}
				if got := popMathCoverageInt(t, st); got != tt.want {
					t.Fatalf("RSHIFTFLOOR result = %d, want %d", got, tt.want)
				}
			})
		}
	})

	t.Run("DynamicErrors", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 7)
		assertMathCoverageVMError(t, RSHIFTFLOOR().Interpret(st), vmerr.CodeStackUnderflow)

		st = newMathCoverageState()
		pushMathCoverageInts(t, st, 7, 257)
		assertMathCoverageVMError(t, RSHIFTFLOOR().Interpret(st), vmerr.CodeRangeCheck)

		st = newMathCoverageState()
		pushMathCoverageInts(t, st, 7)
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN shift: %v", err)
		}
		assertMathCoverageVMError(t, RSHIFTFLOOR().Interpret(st), vmerr.CodeRangeCheck)

		st = newMathCoverageState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN value: %v", err)
		}
		pushMathCoverageInts(t, st, 0)
		assertMathCoverageVMError(t, RSHIFTFLOOR().Interpret(st), vmerr.CodeIntOverflow)

		st = newMathCoverageState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN value: %v", err)
		}
		pushMathCoverageInts(t, st, 13)
		if err := RSHIFTFLOOR().Interpret(st); err != nil {
			t.Fatalf("RSHIFTFLOOR NaN value: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != -1 {
			t.Fatalf("RSHIFTFLOOR NaN value result = %d, want -1", got)
		}
	})

	t.Run("ImmediateBoundaries", func(t *testing.T) {
		tests := []struct {
			name    string
			encoded uint64
			x       int64
			want    int64
			text    string
		}{
			{name: "decoded_1", encoded: 0, x: -7, want: -4, text: "1 RSHIFT#"},
			{name: "decoded_256_positive", encoded: 255, x: 7, want: 0, text: "256 RSHIFT#"},
			{name: "decoded_256_negative", encoded: 255, x: -1, want: -1, text: "256 RSHIFT#"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				op := RSHIFTCODEFLOOR(1)
				code := cell.BeginCell().MustStoreUInt(0xA934, 16).MustStoreUInt(tt.encoded, 8).EndCell().MustBeginParse()
				if err := op.Deserialize(code); err != nil {
					t.Fatalf("deserialize RSHIFT# floor: %v", err)
				}
				if got := op.SerializeText(); got != tt.text {
					t.Fatalf("SerializeText = %q, want %q", got, tt.text)
				}

				st := newMathCoverageState()
				pushMathCoverageInts(t, st, tt.x)
				if err := op.Interpret(st); err != nil {
					t.Fatalf("RSHIFT# floor interpret: %v", err)
				}
				if got := popMathCoverageInt(t, st); got != tt.want {
					t.Fatalf("RSHIFT# floor result = %d, want %d", got, tt.want)
				}
			})
		}
	})

	t.Run("ImmediateNaNReturnsZero", func(t *testing.T) {
		st := newMathCoverageState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN value: %v", err)
		}
		if err := RSHIFTCODEFLOOR(1).Interpret(st); err != nil {
			t.Fatalf("RSHIFT# floor NaN value: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != 0 {
			t.Fatalf("RSHIFT# floor NaN value result = %d, want 0", got)
		}

		st = newMathCoverageState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN value: %v", err)
		}
		op := RSHIFTCODEFLOOR(1)
		if err := op.Deserialize(cell.BeginCell().MustStoreUInt(0xA9340C, 24).EndCell().MustBeginParse()); err != nil {
			t.Fatalf("deserialize RSHIFT# floor 13: %v", err)
		}
		if err := op.Interpret(st); err != nil {
			t.Fatalf("RSHIFT# floor NaN value shift 13: %v", err)
		}
		if got := popMathCoverageInt(t, st); got != -1 {
			t.Fatalf("RSHIFT# floor NaN value shift 13 result = %d, want -1", got)
		}
	})
}

func TestRshiftFloorMatchesBigIntArithmeticShift(t *testing.T) {
	for _, x := range []int64{-1025, -257, -7, -1, 0, 1, 7, 257, 1025} {
		for _, shift := range []uint{0, 1, 2, 8, 255, 256} {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, x, int64(shift))
			if err := RSHIFTFLOOR().Interpret(st); err != nil {
				t.Fatalf("RSHIFTFLOOR x=%d shift=%d: %v", x, shift, err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop result x=%d shift=%d: %v", x, shift, err)
			}
			want := new(big.Int).Rsh(big.NewInt(x), shift)
			if got.Cmp(want) != 0 {
				t.Fatalf("x=%d shift=%d result=%s want=%s", x, shift, got, want)
			}
		}
	}
}
