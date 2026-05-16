package stack

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestXCHG0L(t *testing.T) {
	st := newStack(1, 2, 3, 4, 5)
	if err := XCHG0L(3).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("XCHG0L failed: %v", err)
	}
	got := popInts(t, st, 5)
	want := []int64{2, 4, 3, 5, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestPUSHL(t *testing.T) {
	st := newStack(1, 2, 3, 4, 5)
	if err := PUSHL(3).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("PUSHL failed: %v", err)
	}
	got := popInts(t, st, 6)
	want := []int64{2, 5, 4, 3, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestPOPL(t *testing.T) {
	st := newStack(1, 2, 3, 4, 5)
	if err := POPL(3).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("POPL failed: %v", err)
	}
	got := popInts(t, st, 4)
	want := []int64{4, 3, 5, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
