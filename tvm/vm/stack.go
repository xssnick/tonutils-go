package vm

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"math/big"
)

type Null struct{}

type SendMsgAction struct {
	Mode uint8
	Msg  *cell.Cell
}

type Stack struct {
	elems []*Elem
}

type Elem struct {
	value any
}

func NewStack() *Stack {
	return &Stack{}
}

func (e *Elem) Copy() *Elem {
	return &Elem{
		value: e.value,
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
		return vmerr.ErrIntOverflow
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
		return vmerr.ErrStackOverflow
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
	case nil:
	default:
		if _, ok := val.(Continuation); !ok {
			return vmerr.ErrTypeCheck
		}
	}

	s.elems = append([]*Elem{{value: val}}, s.elems...)
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
		return vmerr.ErrStackUnderflow
	}

	if len(s.elems)+num >= 255 {
		return vmerr.ErrStackOverflow
	}

	s.elems = append(s.elems, from.elems[:num]...)
	from.elems = from.elems[num:]

	return nil
}

func (s *Stack) PushAt(at int) error {
	if at < 0 || at >= len(s.elems) {
		return vmerr.ErrStackUnderflow
	}
	return s.PushAny(s.elems[at].value)
}

func (s *Stack) Drop(num int) error {
	if len(s.elems) < num {
		return vmerr.ErrStackUnderflow
	}
	s.elems = s.elems[num:]
	return nil
}

func (s *Stack) DropAfter(num int) error {
	if len(s.elems) < num {
		return vmerr.ErrStackUnderflow
	}
	s.elems = s.elems[:num]
	return nil
}

func (s *Stack) PopSwapAt(at int) (any, error) {
	if at < 0 || at >= len(s.elems) {
		return nil, vmerr.ErrStackUnderflow
	}

	se := s.elems[at]
	s.elems[at], s.elems[len(s.elems)-1] = s.elems[len(s.elems)-1], s.elems[at]
	s.elems = s.elems[:len(s.elems)-1]
	return se.value, nil
}

func (s *Stack) Set(at int, what any) error {
	if at < 0 || at >= len(s.elems) {
		return vmerr.ErrStackUnderflow
	}
	s.elems[at].value = what
	return nil
}

func (s *Stack) PopAny() (any, error) {
	if len(s.elems) == 0 {
		return nil, vmerr.ErrStackUnderflow
	}
	e := s.elems[0]
	s.elems = s.elems[1:]
	return e.value, nil
}

func (s *Stack) PopInt() (*big.Int, error) {
	e, err := s.PopAny()
	if err != nil {
		return nil, err
	}
	if v, ok := e.(*big.Int); !ok {
		return nil, vmerr.ErrTypeCheck
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
		return nil, vmerr.ErrIntOverflow
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
		return nil, vmerr.ErrTypeCheck
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
		return nil, vmerr.ErrTypeCheck
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
		return nil, vmerr.ErrTypeCheck
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
		return nil, vmerr.ErrTypeCheck
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
		return nil, vmerr.ErrTypeCheck
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
		return nil, vmerr.ErrRangeCheck
	}
	return e, nil
}

func (s *Stack) Get(n uint8) any {
	return s.elems[n].value
}

func (s *Stack) Len() int {
	return len(s.elems)
}

func (s *Stack) Exchange(a, b int) error {
	if (a < 0 || a >= len(s.elems)) || (b < 0 || b >= len(s.elems)) {
		return vmerr.ErrStackUnderflow
	}
	s.elems[a], s.elems[b] = s.elems[b], s.elems[a]
	return nil
}

func (s *Stack) String() string {
	var res string
	for i := len(s.elems) - 1; i >= 0; i-- {
		typ := "???"
		val := "???"
		switch x := s.elems[i].value.(type) {
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
	c := &Stack{
		elems: make([]*Elem, len(s.elems)),
	}

	for i, elem := range s.elems {
		c.elems[i] = &Elem{
			value: elem.value,
		}
	}

	return c
}
