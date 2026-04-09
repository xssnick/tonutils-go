package stack

import (
	"io"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err = w.Close(); err != nil {
		t.Fatalf("stdout close failed: %v", err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("stdout read failed: %v", err)
	}
	if err = r.Close(); err != nil {
		t.Fatalf("stdout read close failed: %v", err)
	}

	return string(out)
}

func TestSTRDUMP_PrintsStringAndDoesNotMutateStack(t *testing.T) {
	st := vm.NewStack()
	src := cell.BeginCell().MustStoreSlice([]byte("hello"), 40).EndCell().BeginParse()

	if err := st.PushSlice(src); err != nil {
		t.Fatalf("push slice failed: %v", err)
	}

	out := captureStdout(t, func() {
		if err := STRDUMP().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("STRDUMP failed: %v", err)
		}
	})

	if !strings.Contains(out, "STRDUMP: hello") {
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
		out := captureStdout(t, func() {
			if err := STRDUMP().Interpret(&vm.State{Stack: st}); err != nil {
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

		out := captureStdout(t, func() {
			if err := STRDUMP().Interpret(&vm.State{Stack: st}); err != nil {
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
		if err := st.PushSlice(cell.BeginCell().MustStoreUInt(0b101, 3).EndCell().BeginParse()); err != nil {
			t.Fatalf("push slice failed: %v", err)
		}

		out := captureStdout(t, func() {
			if err := STRDUMP().Interpret(&vm.State{Stack: st}); err != nil {
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
