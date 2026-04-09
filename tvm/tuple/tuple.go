package tuple

import (
	"math/big"
	"reflect"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type tupleData struct {
	val       []any
	bindingID any
}

type Tuple struct {
	data *tupleData
}

func NewTuple(val ...any) *Tuple {
	cp := append([]any(nil), val...)
	return &Tuple{data: &tupleData{val: cp}}
}

func NewTupleSized(size int) Tuple {
	if size <= 0 {
		return Tuple{}
	}
	return Tuple{data: &tupleData{val: make([]any, size)}}
}

func (t *Tuple) Len() int {
	if t == nil || t.data == nil {
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

func (t *Tuple) BindingID() any {
	if t == nil || t.data == nil {
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
	if t == nil || t.data == nil {
		if bindingID == nil {
			return Tuple{}
		}
		return Tuple{data: &tupleData{bindingID: bindingID}}
	}
	if bindingIDsEqual(t.data.bindingID, bindingID) {
		return Tuple{data: t.data}
	}
	return Tuple{data: &tupleData{val: t.data.val, bindingID: bindingID}}
}

func cloneTupleLeaf(val any) any {
	switch v := val.(type) {
	case *big.Int:
		return new(big.Int).Set(v)
	case *cell.Slice:
		return v.Copy()
	case *cell.Builder:
		return v.Copy()
	default:
		return val
	}
}

func (t *Tuple) Index(i int) (any, error) {
	if t == nil || t.data == nil || i < 0 || i >= len(t.data.val) {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}
	return cloneTupleLeaf(t.data.val[i]), nil
}

func (t *Tuple) RawIndex(i int) (any, error) {
	if t == nil || t.data == nil || i < 0 || i >= len(t.data.val) {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}
	return t.data.val[i], nil
}

func (t *Tuple) Set(i int, val any) error {
	if t == nil || t.data == nil || i < 0 || i >= len(t.data.val) {
		return vmerr.Error(vmerr.CodeRangeCheck)
	}

	next := append([]any(nil), t.data.val...)
	next[i] = val
	t.data = &tupleData{val: next}
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
	t.data = &tupleData{val: next}
}

func (t *Tuple) PopLast() (any, error) {
	if t == nil || t.data == nil || len(t.data.val) == 0 {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}

	idx := len(t.data.val) - 1
	val := cloneTupleLeaf(t.data.val[idx])
	next := append([]any(nil), t.data.val[:idx]...)
	t.data = &tupleData{val: next}
	return val, nil
}

func (t *Tuple) Append(val any) {
	next := make([]any, t.Len()+1)
	if t != nil && t.data != nil {
		copy(next, t.data.val)
	}
	next[len(next)-1] = val
	t.data = &tupleData{val: next}
}
