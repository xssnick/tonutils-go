package stack

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestSWAP(t *testing.T) {
	st := newStack(1, 2)
	if err := SWAP().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("SWAP failed: %v", err)
	}
	got := popInts(t, st, 2)
	want := []int64{1, 2}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v at %d, got %v", want, i, got)
		}
	}
}

func TestDUP(t *testing.T) {
	st := newStack(7)
	if err := DUP().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("DUP failed: %v", err)
	}
	got := popInts(t, st, 2)
	want := []int64{7, 7}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestOVER(t *testing.T) {
	st := newStack(1, 2)
	if err := OVER().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("OVER failed: %v", err)
	}
	got := popInts(t, st, 3)
	want := []int64{1, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestNIP(t *testing.T) {
	st := newStack(1, 2, 3)
	if err := NIP().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("NIP failed: %v", err)
	}
	got := popInts(t, st, 2)
	want := []int64{3, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestCONDSELReturnsFirstValueForTrueFlag(t *testing.T) {
	st := newStack(1, 111, 222)
	if err := CONDSEL().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("CONDSEL failed: %v", err)
	}

	got := popInts(t, st, 1)
	want := []int64{111}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestCONDSELReturnsSecondValueForFalseFlag(t *testing.T) {
	st := newStack(0, 111, 222)
	if err := CONDSEL().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("CONDSEL failed: %v", err)
	}

	got := popInts(t, st, 1)
	want := []int64{222}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
