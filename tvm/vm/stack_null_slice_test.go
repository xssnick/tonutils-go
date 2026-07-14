package vm

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestStackPreservesNullSliceTag(t *testing.T) {
	var nullSlice *cell.Slice
	stack := NewStack()
	if err := stack.PushOwnedSlice(nullSlice); err != nil {
		t.Fatalf("PushOwnedSlice failed: %v", err)
	}
	if err := stack.PushAt(0); err != nil {
		t.Fatalf("PushAt failed: %v", err)
	}

	assertNullSliceStackValue(t, stack)
	assertNullSliceStackValue(t, stack)

	if err := stack.PushOwnedSlice(nullSlice); err != nil {
		t.Fatalf("PushOwnedSlice failed: %v", err)
	}
	copied := stack.Copy()
	assertNullSliceStackValue(t, copied)

	stack.SetTrace(cell.NewTrace(cell.TraceHooks{}))
	assertNullSliceStackValue(t, stack)
}

func TestStackPopSliceRejectsNullReference(t *testing.T) {
	var nullSlice *cell.Slice
	stack := NewStack()
	if err := stack.PushOwnedSlice(nullSlice); err != nil {
		t.Fatalf("PushOwnedSlice failed: %v", err)
	}

	_, err := stack.PopSlice()
	if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeTypeCheck {
		t.Fatalf("PopSlice error = (%d, %t), want type check: %v", code, ok, err)
	}
}

func TestStackNullSliceString(t *testing.T) {
	var nullSlice *cell.Slice
	stack := NewStack()
	if err := stack.PushOwnedSlice(nullSlice); err != nil {
		t.Fatalf("PushOwnedSlice failed: %v", err)
	}
	if got := stack.String(); !strings.Contains(got, "null [slice]") {
		t.Fatalf("Stack.String() = %q, want null slice marker", got)
	}
}

func TestTuplePreservesNullSliceTag(t *testing.T) {
	var nullSlice *cell.Slice
	tup := tuple.NewTupleValue(nullSlice)
	value, err := tup.Index(0)
	if err != nil {
		t.Fatalf("Tuple.Index failed: %v", err)
	}
	got, ok := value.(*cell.Slice)
	if !ok || got != nil {
		t.Fatalf("tuple value = %T %v, want null slice reference", value, value)
	}
}

func assertNullSliceStackValue(t *testing.T, stack *Stack) {
	t.Helper()

	value, err := stack.PopAny()
	if err != nil {
		t.Fatalf("PopAny failed: %v", err)
	}
	got, ok := value.(*cell.Slice)
	if !ok || got != nil {
		t.Fatalf("stack value = %T %v, want null slice reference", value, value)
	}
}
