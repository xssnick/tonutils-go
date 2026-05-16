package stack

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestXC2PU(t *testing.T) {
	st := newStack(1, 2, 3, 4, 5, 6, 7, 8)
	if err := XC2PU(3, 4, 2).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("XC2PU failed: %v", err)
	}
	got := popInts(t, st, 9)
	want := []int64{6, 4, 5, 6, 7, 8, 3, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestXCPUXC(t *testing.T) {
	st := newStack(1, 2, 3, 4, 5, 6, 7, 8)
	if err := XCPUXC(3, 4, 2).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("XCPUXC failed: %v", err)
	}
	got := popInts(t, st, 9)
	want := []int64{5, 4, 8, 6, 7, 4, 3, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestXCPU2(t *testing.T) {
	st := newStack(1, 2, 3, 4, 5, 6, 7, 8)
	if err := XCPU2(4, 3, 2).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("XCPU2 failed: %v", err)
	}
	got := popInts(t, st, 10)
	want := []int64{6, 5, 4, 7, 6, 5, 8, 3, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestPUXC2(t *testing.T) {
	st := newStack(1, 2, 3, 4, 5, 6, 7, 8)
	if err := PUXC2(4, 3, 2).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("PUXC2 failed: %v", err)
	}
	got := popInts(t, st, 9)
	want := []int64{4, 6, 7, 8, 5, 4, 3, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestPUXCPU(t *testing.T) {
	st := newStack(1, 2, 3, 4, 5, 6, 7, 8)
	if err := PUXCPU(4, 3, 2).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("PUXCPU failed: %v", err)
	}
	got := popInts(t, st, 10)
	want := []int64{7, 6, 4, 7, 8, 5, 4, 3, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestPU2XC(t *testing.T) {
	st := newStack(1, 2, 3, 4, 5, 6, 7, 8)
	if err := PU2XC(4, 3, 2).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("PU2XC failed: %v", err)
	}
	got := popInts(t, st, 10)
	want := []int64{4, 6, 8, 7, 6, 5, 4, 3, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestPUSH3(t *testing.T) {
	st := newStack(1, 2, 3, 4, 5, 6, 7, 8)
	if err := PUSH3(2, 3, 4).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("PUSH3 failed: %v", err)
	}
	got := popInts(t, st, 11)
	want := []int64{4, 5, 6, 8, 7, 6, 5, 4, 3, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
