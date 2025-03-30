package vm

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"math/big"
	"reflect"
)

type Null struct{}

type SendMsgAction struct {
	Mode uint8
	Msg  *cell.Cell
}

type Stack struct {
	elems []any
}

func NewStack() *Stack {
	return &Stack{
		elems: make([]any, 0, 256),
	}
}

func (s *Stack) PushBool(val bool) error {
	if val {
		return s.PushAny(big.NewInt(-1))
	}
	return s.PushAny(big.NewInt(0))
}

func (s *Stack) PushBuilder(val *cell.Builder) error {
	return s.PushAny(val)
}

func (s *Stack) PushSlice(val *cell.Slice) error {
	return s.PushAny(val)
}

func (s *Stack) PushCell(val *cell.Cell) error {
	return s.PushAny(val)
}

func (s *Stack) PushContinuation(val Continuation) error {
	return s.PushAny(val)
}

func (s *Stack) PushInt(val *big.Int) error {
	if val.BitLen() > 256 { // 257th bit is sign
		return vmerr.Error(vmerr.CodeIntOverflow)
	}
	return s.PushAny(val)
}

func (s *Stack) PushIntQuiet(val *big.Int) error {
	if val.BitLen() > 256 { // 257th bit is sign
		val = nil // NaN
	}
	return s.PushAny(val)
}

func (s *Stack) PushAny(val any) error {
	if len(s.elems) >= 255 {
		return vmerr.Error(vmerr.CodeStackOverflow)
	}

	switch t := val.(type) {
	case *big.Int:
		// TODO: maybe optimize
		val = new(big.Int).Set(t) // copy for safety
	case *cell.Cell:
	case *cell.Builder:
		val = t.Copy()
	case *cell.Slice:
		val = t.Copy()
	case tuple.Tuple:
		val = t.Copy()
	case nil:
	default:
		if c, ok := val.(Continuation); !ok {
			return vmerr.Error(vmerr.CodeTypeCheck, "type check failed: "+reflect.TypeOf(val).String())
		} else {
			val = c.Copy()
		}
	}

	s.elems = append(s.elems, val)
	return nil
}

func (s *Stack) SplitTop(top, drop int) (*Stack, error) {
	n := s.Len()
	if top >= n || drop > n-top {
		return NewStack(), nil
	}

	newStack := NewStack()
	if top != 0 {
		if err := newStack.MoveFrom(s, top); err != nil {
			return nil, err
		}
	}
	if drop != 0 {
		if err := s.Drop(drop); err != nil {
			return nil, err
		}
	}

	return newStack, nil
}

func (s *Stack) Clear() {
	s.elems = s.elems[:0]
}

func (s *Stack) MoveFrom(from *Stack, num int) error {
	if len(from.elems) < num {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	if len(s.elems)+num >= 255 {
		return vmerr.Error(vmerr.CodeStackOverflow)
	}

	s.elems = append(s.elems, from.elems[:num]...)
	from.elems = from.elems[num:]

	return nil
}

func (s *Stack) index(i int) int {
	return len(s.elems) - 1 - i
}

func (s *Stack) PushAt(at int) error {
	if at < 0 || at >= len(s.elems) {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
	return s.PushAny(s.elems[s.index(at)])
}

func (s *Stack) Drop(num int) error {
	if len(s.elems) < num {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
	s.elems = s.elems[:len(s.elems)-num]
	return nil
}

func (s *Stack) DropAfter(num int) error {
	if len(s.elems) < num {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
	s.elems = s.elems[len(s.elems)-num:]
	return nil
}

func (s *Stack) PopSwapAt(at int) error {
	if at < 0 || at >= len(s.elems) {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	s.elems[s.index(at)] = s.elems[len(s.elems)-1]
	s.elems = s.elems[:len(s.elems)-1]
	return nil
}

func (s *Stack) PopAny() (any, error) {
	if len(s.elems) == 0 {
		return nil, vmerr.Error(vmerr.CodeStackUnderflow)
	}
	e := s.elems[len(s.elems)-1]
	s.elems = s.elems[:len(s.elems)-1]
	return e, nil
}

func (s *Stack) PopInt() (*big.Int, error) {
	e, err := s.PopAny()
	if err != nil {
		return nil, err
	}
	if v, ok := e.(*big.Int); !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	} else {
		return v, nil
	}
}

func (s *Stack) PopIntFinite() (*big.Int, error) {
	e, err := s.PopInt()
	if err != nil {
		return nil, err
	}
	if e == nil { // nil = non valid (NaN)
		return nil, vmerr.Error(vmerr.CodeIntOverflow)
	}
	return e, nil
}

func (s *Stack) PopBool() (bool, error) {
	e, err := s.PopIntFinite()
	if err != nil {
		return false, err
	}
	return e.Sign() != 0, nil
}

func (s *Stack) PopCell() (*cell.Cell, error) {
	e, err := s.PopAny()
	if err != nil {
		return nil, err
	}
	if v, ok := e.(*cell.Cell); !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	} else {
		return v, nil
	}
}

func (s *Stack) PopMaybeCell() (*cell.Cell, error) {
	e, err := s.PopAny()
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, nil
	}
	if v, ok := e.(*cell.Cell); !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	} else {
		return v, nil
	}
}

func (s *Stack) PopContinuation() (Continuation, error) {
	e, err := s.PopAny()
	if err != nil {
		return nil, err
	}
	if v, ok := e.(Continuation); !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	} else {
		return v, nil
	}
}

func (s *Stack) PopBuilder() (*cell.Builder, error) {
	e, err := s.PopAny()
	if err != nil {
		return nil, err
	}
	if v, ok := e.(*cell.Builder); !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	} else {
		return v, nil
	}
}

func (s *Stack) PopSlice() (*cell.Slice, error) {
	e, err := s.PopAny()
	if err != nil {
		return nil, err
	}
	if v, ok := e.(*cell.Slice); !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	} else {
		return v, nil
	}
}

func (s *Stack) PopIntRange(min, max int64) (*big.Int, error) {
	e, err := s.PopInt()
	if err != nil {
		return nil, err
	}

	if e.Cmp(big.NewInt(min)) < 0 || e.Cmp(big.NewInt(max)) > 0 {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}
	return e, nil
}

func (s *Stack) Get(at int) (any, error) {
	if at < 0 || at >= len(s.elems) {
		return nil, vmerr.Error(vmerr.CodeStackUnderflow)
	}
	return s.elems[s.index(at)], nil
}

func (s *Stack) Len() int {
	return len(s.elems)
}

func (s *Stack) Exchange(a, b int) error {
	if (a < 0 || a >= len(s.elems)) || (b < 0 || b >= len(s.elems)) {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
	aIdx, bIdx := s.index(a), s.index(b)
	s.elems[aIdx], s.elems[bIdx] = s.elems[bIdx], s.elems[aIdx]
	return nil
}

func (s *Stack) String() string {
	if len(s.elems) == 0 {
		return "[empty stack]"
	}

	var res string
	for i := len(s.elems) - 1; i >= 0; i-- {
		typ := "???"
		val := "???"
		switch x := s.elems[s.index(i)].(type) {
		case nil:
			typ = "nil"
			val = "nil"
		case *big.Int:
			typ = "int"
			val = x.String()
		case *cell.Slice:
			typ = "slice"
			val = x.MustToCell().Dump()
		case *cell.Builder:
			typ = "builder"
			val = x.EndCell().Dump()
		case *cell.Cell:
			typ = "cell"
			val = x.Dump()
		}

		res += fmt.Sprintf("s%d = %s [%s]\n", i, val, typ)
	}
	return res
}

func (s *Stack) Copy() *Stack {
	c := NewStack()

	for _, elem := range s.elems {
		_ = c.PushAny(elem)
	}

	return c
}
