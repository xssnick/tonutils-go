package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestPICK(t *testing.T) {
	st := newStack(1, 2, 3, 4)
	if err := st.PushAny(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push index: %v", err)
	}
	if err := PICK().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("PICK failed: %v", err)
	}
	got := popInts(t, st, 5)
	want := []int64{2, 4, 3, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestROLL(t *testing.T) {
	st := newStack(1, 2, 3, 4)
	if err := st.PushAny(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push index: %v", err)
	}
	if err := ROLL().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("ROLL failed: %v", err)
	}
	got := popInts(t, st, 4)
	want := []int64{2, 4, 3, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestROLLREV(t *testing.T) {
	st := newStack(1, 2, 3, 4)
	if err := st.PushAny(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push index: %v", err)
	}
	if err := ROLLREV().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("ROLLREV failed: %v", err)
	}
	got := popInts(t, st, 4)
	want := []int64{3, 2, 4, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestBLKSWX(t *testing.T) {
	st := newStack(1, 2, 3)
	if err := st.PushAny(big.NewInt(1)); err != nil {
		t.Fatalf("failed to push x: %v", err)
	}
	if err := st.PushAny(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push y: %v", err)
	}
	if err := BLKSWX().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("BLKSWX failed: %v", err)
	}
	got := popInts(t, st, 3)
	want := []int64{1, 3, 2}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestREVX(t *testing.T) {
	st := newStack(1, 2, 3, 4)
	if err := st.PushAny(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push x: %v", err)
	}
	if err := st.PushAny(big.NewInt(1)); err != nil {
		t.Fatalf("failed to push y: %v", err)
	}
	if err := REVX().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("REVX failed: %v", err)
	}
	got := popInts(t, st, 4)
	want := []int64{4, 2, 3, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestDROPX(t *testing.T) {
	st := newStack(1, 2, 3, 4)
	if err := st.PushAny(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push count: %v", err)
	}
	if err := DROPX().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("DROPX failed: %v", err)
	}
	got := popInts(t, st, 2)
	want := []int64{2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestXCHGX(t *testing.T) {
	st := newStack(1, 2, 3, 4)
	if err := st.PushAny(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push index: %v", err)
	}
	if err := XCHGX().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("XCHGX failed: %v", err)
	}
	got := popInts(t, st, 4)
	want := []int64{2, 3, 4, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestDEPTH(t *testing.T) {
	st := newStack(1, 2, 3)
	if err := DEPTH().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("DEPTH failed: %v", err)
	}
	got := popInts(t, st, 1)
	if got[0] != 3 {
		t.Fatalf("expected depth 3, got %v", got[0])
	}
	if st.Len() != 3 {
		t.Fatalf("expected stack length 3, got %d", st.Len())
	}
}

func TestCHKDEPTH(t *testing.T) {
	st := newStack(1, 2, 3)
	if err := st.PushAny(big.NewInt(3)); err != nil {
		t.Fatalf("failed to push depth: %v", err)
	}
	if err := CHKDEPTH().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("CHKDEPTH failed: %v", err)
	}
	if st.Len() != 3 {
		t.Fatalf("expected stack length 3, got %d", st.Len())
	}
	got := popInts(t, st, 3)
	want := []int64{3, 2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestONLYTOPX(t *testing.T) {
	st := newStack(1, 2, 3, 4)
	if err := st.PushAny(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push count: %v", err)
	}
	if err := ONLYTOPX().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("ONLYTOPX failed: %v", err)
	}
	got := popInts(t, st, 2)
	want := []int64{4, 3}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestONLYX(t *testing.T) {
	st := newStack(1, 2, 3, 4)
	if err := st.PushAny(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push count: %v", err)
	}
	if err := ONLYX().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("ONLYX failed: %v", err)
	}
	got := popInts(t, st, 2)
	want := []int64{2, 1}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
