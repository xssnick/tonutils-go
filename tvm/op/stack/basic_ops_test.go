package stack

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
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

func TestCONDSELPreservesPrefixAndSelectedCell(t *testing.T) {
	prefix := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	x := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	y := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()

	for _, test := range []struct {
		name      string
		condition int64
		want      *cell.Cell
	}{
		{name: "true", condition: -1, want: x},
		{name: "false", condition: 0, want: y},
	} {
		t.Run(test.name, func(t *testing.T) {
			stack := vm.NewStack()
			if err := stack.PushCell(prefix); err != nil {
				t.Fatalf("push prefix: %v", err)
			}
			if err := stack.PushSmallInt(test.condition); err != nil {
				t.Fatalf("push condition: %v", err)
			}
			if err := stack.PushCell(x); err != nil {
				t.Fatalf("push x: %v", err)
			}
			if err := stack.PushCell(y); err != nil {
				t.Fatalf("push y: %v", err)
			}

			if err := CONDSEL().Interpret(&vm.State{Stack: stack}); err != nil {
				t.Fatalf("CONDSEL: %v", err)
			}
			selected, err := stack.PopCell()
			if err != nil {
				t.Fatalf("pop selected cell: %v", err)
			}
			if selected != test.want {
				t.Fatal("CONDSEL did not preserve selected cell identity")
			}
			gotPrefix, err := stack.PopCell()
			if err != nil {
				t.Fatalf("pop prefix: %v", err)
			}
			if gotPrefix != prefix || stack.Len() != 0 {
				t.Fatal("CONDSEL changed the stack prefix")
			}
		})
	}
}

func TestCONDSELConsumesOperandsOnConditionTypeError(t *testing.T) {
	stack := newStack(99)
	if err := stack.PushCell(cell.BeginCell().EndCell()); err != nil {
		t.Fatalf("push invalid condition: %v", err)
	}
	if err := stack.PushSmallInt(11); err != nil {
		t.Fatalf("push x: %v", err)
	}
	if err := stack.PushSmallInt(22); err != nil {
		t.Fatalf("push y: %v", err)
	}

	err := CONDSEL().Interpret(&vm.State{Stack: stack})
	if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeTypeCheck {
		t.Fatalf("CONDSEL error = %v, want type check", err)
	}
	if stack.Len() != 1 {
		t.Fatalf("stack len after type error = %d, want 1", stack.Len())
	}
	if got := popInts(t, stack, 1); got[0] != 99 {
		t.Fatalf("stack prefix after type error = %v, want 99", got)
	}
}
