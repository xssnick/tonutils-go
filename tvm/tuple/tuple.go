package tuple

import "github.com/xssnick/tonutils-go/tvm/vmerr"

type Tuple struct {
	val []any
}

func NewTuple(val ...any) *Tuple {
	return &Tuple{val}
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
	if i >= len(t.val) {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}
	return t.val[i], nil
}

func (t *Tuple) Append(val any) {
	t.val = append(t.val, val)
}
