package tuple

import "github.com/xssnick/tonutils-go/tvm/vmerr"

type Tuple struct {
	val []any
}

func NewTuple(val ...any) *Tuple {
	return &Tuple{val}
}

func NewTupleSized(size int) Tuple {
	if size < 0 {
		size = 0
	}
	return Tuple{val: make([]any, size)}
}

func (t *Tuple) Len() int {
	return len(t.val)
}

func (t *Tuple) Copy() Tuple {
	tp := Tuple{make([]any, len(t.val))}
	copy(tp.val, t.val)
	return tp
}

func (t *Tuple) Index(i int) (any, error) {
	if i < 0 || i >= len(t.val) {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}
	return t.val[i], nil
}

func (t *Tuple) Set(i int, val any) error {
	if i < 0 || i >= len(t.val) {
		return vmerr.Error(vmerr.CodeRangeCheck)
	}
	t.val[i] = val
	return nil
}

func (t *Tuple) Resize(size int) {
	if size < 0 {
		size = 0
	}

	if size <= len(t.val) {
		for i := size; i < len(t.val); i++ {
			t.val[i] = nil
		}
		t.val = t.val[:size]
		return
	}

	if size <= cap(t.val) {
		t.val = append(t.val, make([]any, size-len(t.val))...)
		return
	}

	newVal := make([]any, size)
	copy(newVal, t.val)
	t.val = newVal
}

func (t *Tuple) PopLast() (any, error) {
	if len(t.val) == 0 {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}

	idx := len(t.val) - 1
	val := t.val[idx]
	t.val[idx] = nil
	t.val = t.val[:idx]
	return val, nil
}

func (t *Tuple) Append(val any) {
	t.val = append(t.val, val)
}
