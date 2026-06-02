package stack

import (
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
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
