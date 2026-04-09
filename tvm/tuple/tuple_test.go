package tuple

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTupleBindingHelpers(t *testing.T) {
	var nilTuple *Tuple

	if nilTuple.Len() != 0 {
		t.Fatalf("nil tuple len = %d, want 0", nilTuple.Len())
	}
	if nilTuple.BindingID() != nil {
		t.Fatalf("nil tuple binding id = %v, want nil", nilTuple.BindingID())
	}
	if !bindingIDsEqual(nil, nil) {
		t.Fatal("nil binding ids should be equal")
	}
	if bindingIDsEqual([]int{1}, []int{1}) {
		t.Fatal("non-comparable binding ids should not be equal")
	}

	id := struct{ N int }{N: 7}
	if !bindingIDsEqual(id, id) {
		t.Fatal("comparable binding ids should be equal")
	}

	bound := nilTuple.WithBindingID(id)
	if !bound.HasBindingID(id) {
		t.Fatal("tuple should keep assigned binding id")
	}

	tup := NewTuple("a")
	if tup.HasBindingID(id) {
		t.Fatal("fresh tuple should not have binding id")
	}

	withID := tup.WithBindingID(id)
	if !withID.HasBindingID(id) {
		t.Fatal("tuple should report assigned binding id")
	}

	withSameID := withID.WithBindingID(id)
	if withSameID.BindingID() != id {
		t.Fatalf("binding id = %v, want %v", withSameID.BindingID(), id)
	}

	empty := NewTupleSized(0)
	if empty.Len() != 0 {
		t.Fatal("zero-sized tuple should be empty")
	}
}

func TestTupleIndexClonesMutableLeaves(t *testing.T) {
	intVal := big.NewInt(11)
	sliceVal := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse()
	builderVal := cell.BeginCell().MustStoreUInt(0xBB, 8)

	tup := NewTuple(intVal, sliceVal, builderVal)

	gotIntAny, err := tup.Index(0)
	if err != nil {
		t.Fatalf("index int: %v", err)
	}
	gotInt := gotIntAny.(*big.Int)
	gotInt.Add(gotInt, big.NewInt(5))

	rawIntAny, err := tup.RawIndex(0)
	if err != nil {
		t.Fatalf("raw index int: %v", err)
	}
	rawInt := rawIntAny.(*big.Int)
	if rawInt.Int64() != 11 {
		t.Fatalf("raw int changed to %d", rawInt.Int64())
	}

	gotSliceAny, err := tup.Index(1)
	if err != nil {
		t.Fatalf("index slice: %v", err)
	}
	gotSlice := gotSliceAny.(*cell.Slice)
	if _, err = gotSlice.LoadUInt(8); err != nil {
		t.Fatalf("load from copied slice: %v", err)
	}

	rawSliceAny, err := tup.RawIndex(1)
	if err != nil {
		t.Fatalf("raw index slice: %v", err)
	}
	rawSlice := rawSliceAny.(*cell.Slice)
	if bits := rawSlice.BitsLeft(); bits != 8 {
		t.Fatalf("raw slice bits left = %d, want 8", bits)
	}

	gotBuilderAny, err := tup.Index(2)
	if err != nil {
		t.Fatalf("index builder: %v", err)
	}
	gotBuilder := gotBuilderAny.(*cell.Builder)
	if err = gotBuilder.StoreUInt(0xCC, 8); err != nil {
		t.Fatalf("mutate copied builder: %v", err)
	}

	rawBuilderAny, err := tup.RawIndex(2)
	if err != nil {
		t.Fatalf("raw index builder: %v", err)
	}
	rawBuilder := rawBuilderAny.(*cell.Builder)
	if bits := rawBuilder.BitsUsed(); bits != 8 {
		t.Fatalf("raw builder bits used = %d, want 8", bits)
	}
}

func TestTupleMutationHelpers(t *testing.T) {
	tup := NewTupleSized(2)
	if err := tup.Set(0, "left"); err != nil {
		t.Fatalf("set index 0: %v", err)
	}
	if err := tup.Set(1, "right"); err != nil {
		t.Fatalf("set index 1: %v", err)
	}

	copyTup := tup.Copy()
	if err := copyTup.Set(1, "changed"); err != nil {
		t.Fatalf("set on copied tuple: %v", err)
	}

	origAny, err := tup.RawIndex(1)
	if err != nil {
		t.Fatalf("raw index original: %v", err)
	}
	if origAny.(string) != "right" {
		t.Fatalf("original tuple changed to %q", origAny.(string))
	}

	copyAny, err := copyTup.RawIndex(1)
	if err != nil {
		t.Fatalf("raw index copy: %v", err)
	}
	if copyAny.(string) != "changed" {
		t.Fatalf("copied tuple value = %q, want changed", copyAny.(string))
	}

	tup.Append("tail")
	if tup.Len() != 3 {
		t.Fatalf("tuple len after append = %d, want 3", tup.Len())
	}

	last, err := tup.PopLast()
	if err != nil {
		t.Fatalf("pop last: %v", err)
	}
	if last.(string) != "tail" {
		t.Fatalf("last value = %q, want tail", last.(string))
	}
	if tup.Len() != 2 {
		t.Fatalf("tuple len after pop = %d, want 2", tup.Len())
	}

	tup.Resize(4)
	if tup.Len() != 4 {
		t.Fatalf("tuple len after grow = %d, want 4", tup.Len())
	}
	tup.Resize(-1)
	if tup.Len() != 0 {
		t.Fatalf("tuple len after shrink = %d, want 0", tup.Len())
	}

	if _, err = tup.Index(0); err == nil {
		t.Fatal("expected range error from empty tuple")
	} else {
		var vmErr vmerr.VMError
		if ok := errorAs(err, &vmErr); !ok || vmErr.Code != vmerr.CodeRangeCheck {
			t.Fatalf("expected range check error, got %v", err)
		}
	}

	var nilTuple *Tuple
	if _, err = nilTuple.PopLast(); err == nil {
		t.Fatal("expected pop from nil tuple to fail")
	}
	if err = nilTuple.Set(0, "x"); err == nil {
		t.Fatal("expected set on nil tuple to fail")
	}
}

func errorAs(err error, target interface{}) bool {
	switch t := target.(type) {
	case *vmerr.VMError:
		if err == nil {
			return false
		}
		if vmErr, ok := err.(vmerr.VMError); ok {
			*t = vmErr
			return true
		}
	}
	return false
}
