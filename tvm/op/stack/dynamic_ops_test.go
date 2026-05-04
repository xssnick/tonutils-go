package stack

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
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

func TestLargeDynamicStackOpsChargeParityGas(t *testing.T) {
	tests := []struct {
		name string
		op   vm.OP
		st   *vm.Stack
		want int64
	}{
		{
			name: "ROLL",
			op:   ROLL(),
			st:   newStackWithIndex(260, 256),
			want: 1,
		},
		{
			name: "ROLLREV",
			op:   ROLLREV(),
			st:   newStackWithIndex(260, 256),
			want: 1,
		},
		{
			name: "BLKSWX",
			op:   BLKSWX(),
			st:   newStackWithCounts(300, 200, 60),
			want: 5,
		},
		{
			name: "REVX",
			op:   REVX(),
			st:   newStackWithCounts(300, 256, 0),
			want: 1,
		},
		{
			name: "ONLYTOPX",
			op:   ONLYTOPX(),
			st:   newStackWithIndex(300, 256),
			want: 1,
		},
		{
			name: "ONLYTOPX full depth",
			op:   ONLYTOPX(),
			st:   newStackWithIndex(256, 256),
			want: 0,
		},
		{
			name: "ONLYX no extra gas",
			op:   ONLYX(),
			st:   newStackWithIndex(300, 256),
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &vm.State{
				Stack: tt.st,
				Gas:   vm.GasWithLimit(10_000),
			}
			before := state.Gas.Used()
			if err := tt.op.Interpret(state); err != nil {
				t.Fatalf("%s failed: %v", tt.name, err)
			}
			if got := state.Gas.Used() - before; got != tt.want {
				t.Fatalf("%s extra gas = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestLargeDynamicStackOpsOutOfGasBeforeMutation(t *testing.T) {
	tests := []struct {
		name      string
		op        vm.OP
		st        *vm.Stack
		remaining int64
		wantTop   int64
	}{
		{
			name:      "ROLL",
			op:        ROLL(),
			st:        newStackWithIndex(260, 256),
			remaining: 0,
			wantTop:   260,
		},
		{
			name:      "BLKSWX",
			op:        BLKSWX(),
			st:        newStackWithCounts(300, 200, 60),
			remaining: 4,
			wantTop:   300,
		},
		{
			name:      "REVX",
			op:        REVX(),
			st:        newStackWithCounts(300, 256, 0),
			remaining: 0,
			wantTop:   300,
		},
		{
			name:      "ONLYTOPX",
			op:        ONLYTOPX(),
			st:        newStackWithIndex(300, 256),
			remaining: 0,
			wantTop:   300,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &vm.State{
				Stack: tt.st,
				Gas: vm.Gas{
					Limit:     tt.remaining,
					Base:      tt.remaining,
					Remaining: tt.remaining,
				},
			}
			err := tt.op.Interpret(state)
			if err == nil {
				t.Fatalf("%s should fail with out of gas", tt.name)
			}
			var vmErr vmerr.VMError
			if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeOutOfGas {
				t.Fatalf("%s error = %v, want out of gas", tt.name, err)
			}
			got, err := state.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop top after %s failure: %v", tt.name, err)
			}
			if got.Int64() != tt.wantTop {
				t.Fatalf("%s mutated stack before gas failure: top=%d want=%d", tt.name, got.Int64(), tt.wantTop)
			}
		})
	}
}

func newStackWithIndex(depth, idx int) *vm.Stack {
	st := vm.NewStack()
	for i := 1; i <= depth; i++ {
		if err := st.PushInt(big.NewInt(int64(i))); err != nil {
			panic(err)
		}
	}
	if err := st.PushInt(big.NewInt(int64(idx))); err != nil {
		panic(err)
	}
	return st
}

func newStackWithCounts(depth, x, y int) *vm.Stack {
	st := vm.NewStack()
	for i := 1; i <= depth; i++ {
		if err := st.PushInt(big.NewInt(int64(i))); err != nil {
			panic(err)
		}
	}
	if err := st.PushInt(big.NewInt(int64(x))); err != nil {
		panic(err)
	}
	if err := st.PushInt(big.NewInt(int64(y))); err != nil {
		panic(err)
	}
	return st
}
