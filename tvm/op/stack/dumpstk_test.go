package stack

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func captureTrace(t *testing.T, stack *vm.Stack, fn func(*vm.State)) string {
	t.Helper()

	var lines []string
	state := &vm.State{
		Stack: stack,
		TraceHook: func(step vm.TraceStep) {
			lines = append(lines, step.String())
		},
	}

	fn(state)

	return strings.Join(lines, "\n")
}

func TestSTRDUMP_PrintsStringAndDoesNotMutateStack(t *testing.T) {
	st := vm.NewStack()
	src := cell.BeginCell().MustStoreSlice([]byte("hello"), 40).EndCell().MustBeginParse()

	if err := st.PushSlice(src); err != nil {
		t.Fatalf("push slice failed: %v", err)
	}

	out := captureTrace(t, st, func(state *vm.State) {
		if err := STRDUMP().Interpret(state); err != nil {
			t.Fatalf("STRDUMP failed: %v", err)
		}
	})

	if !strings.Contains(out, "#DEBUG#: hello") {
		t.Fatalf("unexpected output: %q", out)
	}
	if st.Len() != 1 {
		t.Fatalf("expected stack size 1, got %d", st.Len())
	}

	got, err := st.PopSlice()
	if err != nil {
		t.Fatalf("pop slice failed: %v", err)
	}
	if got.BitsLeft() != 40 {
		t.Fatalf("expected 40 bits left, got %d", got.BitsLeft())
	}
	if string(got.MustLoadSlice(40)) != "hello" {
		t.Fatalf("slice was mutated")
	}
}

func TestSTRDUMP_CornerCases(t *testing.T) {
	t.Run("empty stack", func(t *testing.T) {
		st := vm.NewStack()
		out := captureTrace(t, st, func(state *vm.State) {
			if err := STRDUMP().Interpret(state); err != nil {
				t.Fatalf("STRDUMP failed: %v", err)
			}
		})
		if !strings.Contains(out, "s0 is absent") {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("not a slice", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push int failed: %v", err)
		}

		out := captureTrace(t, st, func(state *vm.State) {
			if err := STRDUMP().Interpret(state); err != nil {
				t.Fatalf("STRDUMP failed: %v", err)
			}
		})
		if !strings.Contains(out, "is not a slice") {
			t.Fatalf("unexpected output: %q", out)
		}
		if st.Len() != 1 {
			t.Fatalf("stack was mutated")
		}
	})

	t.Run("not byte aligned", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushSlice(cell.BeginCell().MustStoreUInt(0b101, 3).EndCell().MustBeginParse()); err != nil {
			t.Fatalf("push slice failed: %v", err)
		}

		out := captureTrace(t, st, func(state *vm.State) {
			if err := STRDUMP().Interpret(state); err != nil {
				t.Fatalf("STRDUMP failed: %v", err)
			}
		})
		if !strings.Contains(out, "slice contains not valid bits count") {
			t.Fatalf("unexpected output: %q", out)
		}
		if st.Len() != 1 {
			t.Fatalf("stack was mutated")
		}
	})
}

func TestDUMPAndDEBUGRoundTripAndTrace(t *testing.T) {
	t.Run("DumpPresent", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("push int failed: %v", err)
		}

		op := DUMP(0)
		dst := DUMP(0)
		if err := dst.Deserialize(op.Serialize().EndCell().MustBeginParse()); err != nil {
			t.Fatalf("DUMP deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); got != "DUMP s0" {
			t.Fatalf("unexpected DUMP text: %q", got)
		}
		if got := dst.InstructionBits(); got != 16 {
			t.Fatalf("unexpected DUMP bits: %d", got)
		}

		out := captureTrace(t, st, func(state *vm.State) {
			if err := dst.Interpret(state); err != nil {
				t.Fatalf("DUMP failed: %v", err)
			}
		})
		if !strings.Contains(out, "s0 = 7 [int]") {
			t.Fatalf("unexpected DUMP trace: %q", out)
		}
		if st.Len() != 1 {
			t.Fatalf("DUMP mutated stack: %d", st.Len())
		}
	})

	t.Run("DumpAbsent", func(t *testing.T) {
		st := vm.NewStack()
		out := captureTrace(t, st, func(state *vm.State) {
			if err := DUMP(3).Interpret(state); err != nil {
				t.Fatalf("DUMP absent failed: %v", err)
			}
		})
		if !strings.Contains(out, "s3 is absent") {
			t.Fatalf("unexpected DUMP absent trace: %q", out)
		}
	})

	t.Run("DumpTruncatedSuffix", func(t *testing.T) {
		err := DUMP(0).Deserialize(cell.BeginCell().MustStoreUInt(0xFE2, 12).EndCell().MustBeginParse())
		if err == nil {
			t.Fatal("expected DUMP truncated suffix to fail")
		}
	})

	t.Run("Debug", func(t *testing.T) {
		op := DEBUG(42)
		dst := DEBUG(0)
		if err := dst.Deserialize(op.Serialize().EndCell().MustBeginParse()); err != nil {
			t.Fatalf("DEBUG deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); got != "DEBUG 42" {
			t.Fatalf("unexpected DEBUG text: %q", got)
		}
		if got := dst.InstructionBits(); got != 16 {
			t.Fatalf("unexpected DEBUG bits: %d", got)
		}

		out := captureTrace(t, vm.NewStack(), func(state *vm.State) {
			if err := dst.Interpret(state); err != nil {
				t.Fatalf("DEBUG failed: %v", err)
			}
		})
		if !strings.Contains(out, "DEBUG 42") {
			t.Fatalf("unexpected DEBUG trace: %q", out)
		}
	})

	t.Run("DebugTruncatedSuffix", func(t *testing.T) {
		err := DEBUG(0).Deserialize(cell.BeginCell().MustStoreUInt(0xFE, 8).EndCell().MustBeginParse())
		if err == nil {
			t.Fatal("expected DEBUG truncated suffix to fail")
		}
	})
}

func TestDEBUGSTRRoundTripAndErrors(t *testing.T) {
	t.Run("RoundTripTraceAndPrefixes", func(t *testing.T) {
		op := DEBUGSTR([]byte("hello"))
		if len(op.GetPrefixes()) == 0 {
			t.Fatal("expected DEBUGSTR prefixes")
		}

		dst := DEBUGSTR(nil)
		if err := dst.Deserialize(op.Serialize().EndCell().MustBeginParse()); err != nil {
			t.Fatalf("DEBUGSTR deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); got != "DEBUGSTR 68656C6C6F" {
			t.Fatalf("unexpected DEBUGSTR text: %q", got)
		}
		if got := dst.(interface{ InstructionBits() int64 }).InstructionBits(); got != 16 {
			t.Fatalf("unexpected DEBUGSTR bits: %d", got)
		}

		out := captureTrace(t, vm.NewStack(), func(state *vm.State) {
			if err := dst.Interpret(state); err != nil {
				t.Fatalf("DEBUGSTR failed: %v", err)
			}
		})
		if !strings.Contains(out, "DEBUGSTR 68656C6C6F") {
			t.Fatalf("unexpected DEBUGSTR trace: %q", out)
		}
	})

	t.Run("EmptyDefaultsToZero", func(t *testing.T) {
		op := DEBUGSTR(nil)
		if got := op.SerializeText(); got != "DEBUGSTR 00" {
			t.Fatalf("unexpected empty DEBUGSTR text: %q", got)
		}
	})

	t.Run("LongInputIsTruncated", func(t *testing.T) {
		op := DEBUGSTR([]byte("0123456789abcdef-extra"))
		if got := op.SerializeText(); got != "DEBUGSTR 30313233343536373839616263646566" {
			t.Fatalf("unexpected truncated DEBUGSTR text: %q", got)
		}
	})

	t.Run("DeserializeShortPrefix", func(t *testing.T) {
		err := DEBUGSTR(nil).Deserialize(cell.BeginCell().EndCell().MustBeginParse())
		if err == nil {
			t.Fatal("expected DEBUGSTR short prefix to fail")
		}
	})

	t.Run("DeserializeMissingLength", func(t *testing.T) {
		err := DEBUGSTR(nil).Deserialize(cell.BeginCell().MustStoreUInt(0xFEF, 12).EndCell().MustBeginParse())
		var tvmErr vmerr.VMError
		if !errors.As(err, &tvmErr) || tvmErr.Code != vmerr.CodeInvalidOpcode {
			t.Fatalf("expected invalid opcode, got %v", err)
		}
	})

	t.Run("DeserializeMissingPayload", func(t *testing.T) {
		err := DEBUGSTR(nil).Deserialize(cell.BeginCell().
			MustStoreUInt(0xFEF, 12).
			MustStoreUInt(1, 4).
			EndCell().
			MustBeginParse())
		var tvmErr vmerr.VMError
		if !errors.As(err, &tvmErr) || tvmErr.Code != vmerr.CodeInvalidOpcode {
			t.Fatalf("expected invalid opcode, got %v", err)
		}
	})
}

func TestDebugValueStringVariants(t *testing.T) {
	ref := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	sl := cell.BeginCell().MustStoreUInt(0xC, 4).EndCell().MustBeginParse()
	builder := cell.BeginCell().MustStoreUInt(0xD, 4)

	tests := []struct {
		name string
		val  any
		want string
	}{
		{name: "nil", val: nil, want: "nil [nil]"},
		{name: "nan", val: vm.NaN{}, want: "NaN [nan]"},
		{name: "int", val: big.NewInt(12), want: "12 [int]"},
		{name: "slice", val: sl, want: "[slice]"},
		{name: "builder", val: builder, want: "[builder]"},
		{name: "cell", val: ref, want: "[cell]"},
		{name: "fallback", val: struct{ A int }{A: 1}, want: "[struct { A int }]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := debugValueString(tt.val); !strings.Contains(got, tt.want) {
				t.Fatalf("debugValueString(%s) = %q, want containing %q", tt.name, got, tt.want)
			}
		})
	}
}
