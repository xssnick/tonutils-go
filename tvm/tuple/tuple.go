package tuple

import (
	"math/big"
	"reflect"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type tupleData struct {
	val                []any
	bindingID          any
	needsValueSnapshot bool
}

type Tuple struct {
	data *tupleData
}

func (t *Tuple) IsNull() bool {
	return t == nil || t.data == nil
}

// NewTupleValue builds a tuple as a value wrapper, which is the preferred form
// for storing and passing Tuple because it already shares its internal data via
// pointer semantics.
func NewTupleValue(val ...any) Tuple {
	cp := append([]any(nil), val...)
	return Tuple{data: newTupleData(cp, nil)}
}

// NewTupleOwned builds a tuple from values owned by the caller. The values slice
// must not be mutated after the call.
func NewTupleOwned(val []any) Tuple {
	return Tuple{data: newTupleData(val, nil)}
}

// NewTupleOwnedBound builds a tuple from caller-owned values with the binding
// ID already set, skipping the intermediate tuple that NewTupleOwned followed
// by WithBindingID would allocate.
func NewTupleOwnedBound(val []any, bindingID any) Tuple {
	return Tuple{data: newTupleData(val, bindingID)}
}

// NewTuple keeps the legacy pointer-returning constructor for compatibility.
func NewTuple(val ...any) *Tuple {
	t := NewTupleValue(val...)
	return &t
}

func NewTupleSized(size int) Tuple {
	if size < 0 {
		panic("negative tuple size")
	}
	// size 0 is a non-null empty tuple, distinct from the null value
	return Tuple{data: &tupleData{val: make([]any, size)}}
}

func newTupleData(val []any, bindingID any) *tupleData {
	data := &tupleData{val: val, bindingID: bindingID}
	for _, item := range val {
		if stackValueNeedsSnapshot(item) {
			data.needsValueSnapshot = true
			break
		}
	}
	return data
}

func stackValueNeedsSnapshot(val any) bool {
	switch v := val.(type) {
	case *big.Int:
		return v == nil
	case *cell.Slice:
		return v != nil
	case *cell.Builder:
		return v != nil
	case Tuple:
		return v.NeedsValueSnapshot()
	default:
		return false
	}
}

func (t *Tuple) Len() int {
	if t.IsNull() {
		return 0
	}
	return len(t.data.val)
}

func (t *Tuple) Copy() Tuple {
	if t == nil {
		return Tuple{}
	}
	return Tuple{data: t.data}
}

// NeedsValueSnapshot reports whether the tuple contains mutable cursor values
// that must be isolated when the tuple crosses a VM ownership boundary. The
// summary is maintained when persistent tuple data is created, so scalar and
// cell-only tuple trees can be rejected in O(1).
func (t *Tuple) NeedsValueSnapshot() bool {
	return !t.IsNull() && t.data.needsValueSnapshot
}

func (t *Tuple) BindingID() any {
	if t.IsNull() {
		return nil
	}
	return t.data.bindingID
}

func bindingIDsEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	ta := reflect.TypeOf(a)
	if ta != reflect.TypeOf(b) || !ta.Comparable() {
		return false
	}
	return a == b
}

func (t *Tuple) HasBindingID(bindingID any) bool {
	return bindingIDsEqual(t.BindingID(), bindingID)
}

func (t *Tuple) WithBindingID(bindingID any) Tuple {
	if t.IsNull() {
		if bindingID == nil {
			return Tuple{}
		}
		return Tuple{data: &tupleData{bindingID: bindingID}}
	}
	if bindingIDsEqual(t.data.bindingID, bindingID) {
		return Tuple{data: t.data}
	}
	return Tuple{data: &tupleData{
		val:                t.data.val,
		bindingID:          bindingID,
		needsValueSnapshot: t.data.needsValueSnapshot,
	}}
}

func cloneTupleLeaf(val any) any {
	switch v := val.(type) {
	case *big.Int:
		if v == nil {
			return nil
		}
		return new(big.Int).Set(v)
	case *cell.Slice:
		if v == nil {
			return v
		}
		return v.Copy()
	case *cell.Builder:
		if v == nil {
			return v
		}
		return v.Copy()
	default:
		return val
	}
}

func (t *Tuple) Index(i int) (any, error) {
	if t.IsNull() || i < 0 || i >= len(t.data.val) {
		return nil, vmerr.Error(vmerr.CodeRangeCheck, "tuple index out of range")
	}
	return cloneTupleLeaf(t.data.val[i]), nil
}

func (t *Tuple) RawIndex(i int) (any, error) {
	if t.IsNull() || i < 0 || i >= len(t.data.val) {
		return nil, vmerr.Error(vmerr.CodeRangeCheck, "tuple index out of range")
	}
	return t.data.val[i], nil
}

func (t *Tuple) Set(i int, val any) error {
	if t.IsNull() || i < 0 || i >= len(t.data.val) {
		return vmerr.Error(vmerr.CodeRangeCheck, "tuple index out of range")
	}

	next := append([]any(nil), t.data.val...)
	next[i] = val
	t.data = newTupleData(next, nil)
	return nil
}

func (t *Tuple) Resize(size int) {
	if size < 0 {
		size = 0
	}

	curLen := t.Len()
	if curLen == size {
		return
	}

	next := make([]any, size)
	if t != nil && t.data != nil {
		copy(next, t.data.val)
	}
	t.data = newTupleData(next, nil)
}

func (t *Tuple) PopLast() (any, error) {
	if t.IsNull() || len(t.data.val) == 0 {
		return nil, vmerr.Error(vmerr.CodeRangeCheck, "tuple index out of range")
	}

	idx := len(t.data.val) - 1
	val := cloneTupleLeaf(t.data.val[idx])
	next := append([]any(nil), t.data.val[:idx]...)
	t.data = newTupleData(next, nil)
	return val, nil
}

func (t *Tuple) Append(val any) {
	next := make([]any, t.Len()+1)
	needsValueSnapshot := stackValueNeedsSnapshot(val)
	if t != nil && t.data != nil {
		copy(next, t.data.val)
		needsValueSnapshot = needsValueSnapshot || t.data.needsValueSnapshot
	}
	next[len(next)-1] = val
	t.data = &tupleData{val: next, needsValueSnapshot: needsValueSnapshot}
}
