package vm

import (
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type Null struct{}

type NaN struct{}

type SendMsgAction struct {
	Mode uint8
	Msg  *cell.Cell
}

type Stack struct {
	elems []any
	trace *cell.Trace
}

var maxTVMInt = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
var minTVMInt = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))

const (
	defaultStackCapacity = 16
	stackStaticIntMin    = -5
	stackStaticIntMax    = 10
	stackStaticIntCount  = stackStaticIntMax - stackStaticIntMin + 1
)

var stackStaticInts = makeStackStaticInts()

var stackIntMinusOne = stackStaticInt(-1)
var stackIntZero = stackStaticInt(0)
var stackIntOne = stackStaticInt(1)

func NewStack() *Stack {
	return newStackWithCap(defaultStackCapacity)
}

func newStackWithCap(capacity int) *Stack {
	return &Stack{
		elems: make([]any, 0, capacity),
	}
}

func makeStackStaticInts() [stackStaticIntCount]*big.Int {
	var vals [stackStaticIntCount]*big.Int
	for i := range vals {
		vals[i] = big.NewInt(int64(stackStaticIntMin + i))
	}
	return vals
}

func stackStaticInt(val int64) *big.Int {
	if val < stackStaticIntMin || val > stackStaticIntMax {
		return nil
	}
	return stackStaticInts[val-stackStaticIntMin]
}

func (s *Stack) SetTrace(trace *cell.Trace) {
	s.trace = trace
	if trace == nil {
		return
	}
	for i, val := range s.elems {
		s.elems[i] = BindValueTrace(val, trace)
	}
}

func bindTupleTrace(t tuple.Tuple, trace *cell.Trace) tuple.Tuple {
	if trace == nil {
		return t
	}
	if t.HasBindingID(trace) {
		return t
	}

	var vals []any
	for i := 0; i < t.Len(); i++ {
		val, err := t.RawIndex(i)
		if err != nil {
			panic(err)
		}
		bound := BindValueTrace(val, trace)
		if vals == nil && sameStackValue(bound, val) {
			continue
		}
		if vals == nil {
			vals = make([]any, t.Len())
			copyTuplePrefix(vals, t, i)
		}
		vals[i] = bound
	}
	if vals == nil {
		return t.WithBindingID(trace)
	}
	return tuple.NewTupleOwnedBound(vals, trace)
}

func sameStackValue(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	ta := reflect.TypeOf(a)
	if ta != reflect.TypeOf(b) || !ta.Comparable() {
		return false
	}
	return a == b
}

func copyTuplePrefix(dst []any, src tuple.Tuple, end int) {
	for i := 0; i < end; i++ {
		val, err := src.RawIndex(i)
		if err != nil {
			panic(err)
		}
		dst[i] = val
	}
}

// BindValueTrace returns val ready to live on a stack or in c7 bound to the
// given cell trace: slices and builders are copied with the trace attached
// (snapshotting their cursor), tuples are rebound recursively and typed nil
// pointers collapse to plain nil, except a null slice reference whose slice
// tag is observable in legacy TVM behavior. Values that already carry the
// trace are returned as-is. A nil trace returns val unchanged.
func BindValueTrace(val any, trace *cell.Trace) any {
	if trace == nil {
		return val
	}

	switch x := val.(type) {
	case *big.Int:
		// normalize typed nils coming from user-built tuples
		if x == nil {
			return nil
		}
		return x
	case *cell.Cell:
		if x == nil {
			return nil
		}
		return x
	case *cell.Slice:
		if x == nil {
			return x
		}
		combined := cell.CombineTraces(x.Trace(), trace)
		if combined == x.Trace() {
			return x
		}
		return x.Copy().SetTrace(combined)
	case *cell.Builder:
		if x == nil {
			return nil
		}
		combined := cell.CombineTraces(x.Trace(), trace)
		if combined == x.Trace() {
			return x
		}
		return x.Copy().SetTrace(combined)
	case tuple.Tuple:
		return bindTupleTrace(x, trace)
	default:
		return val
	}
}

func unbindTupleTrace(t tuple.Tuple, trace *cell.Trace) tuple.Tuple {
	if trace == nil || t.IsNull() {
		return t
	}

	var vals []any
	for i := 0; i < t.Len(); i++ {
		val, err := t.RawIndex(i)
		if err != nil {
			panic(err)
		}
		unbound := unbindValueTrace(val, trace)
		if vals == nil && sameStackValue(unbound, val) {
			continue
		}
		if vals == nil {
			vals = make([]any, t.Len())
			copyTuplePrefix(vals, t, i)
		}
		vals[i] = unbound
	}
	if vals == nil {
		return t.WithBindingID(nil)
	}
	return tuple.NewTupleOwned(vals)
}

func unbindValueTrace(val any, trace *cell.Trace) any {
	if trace == nil {
		return val
	}

	switch x := val.(type) {
	case *cell.Cell:
		if x == nil {
			return nil
		}
		return x.WithTrace(x.Trace().WithoutTrace(trace))
	case *cell.Slice:
		if x == nil {
			return x
		}
		next := x.Trace().WithoutTrace(trace)
		if next == x.Trace() {
			return x
		}
		return x.Copy().SetTrace(next)
	case *cell.Builder:
		if x == nil {
			return nil
		}
		next := x.Trace().WithoutTrace(trace)
		if next == x.Trace() {
			return x
		}
		return x.Copy().SetTrace(next)
	case tuple.Tuple:
		return unbindTupleTrace(x, trace)
	default:
		return val
	}
}

func (s *Stack) WithoutTrace(trace *cell.Trace) *Stack {
	if s == nil || trace == nil {
		return s
	}
	cp := &Stack{
		elems: make([]any, len(s.elems)),
		trace: s.trace.WithoutTrace(trace),
	}
	for i, val := range s.elems {
		cp.elems[i] = unbindValueTrace(val, trace)
	}
	return cp
}

func shareStackValue(val any, trace *cell.Trace) (any, error) {
	switch t := val.(type) {
	case *big.Int:
		if t == nil {
			return nil, nil
		}
		return canonicalStackInt(t), nil
	case NaN:
		return NaN{}, nil
	case *cell.Cell:
		if t == nil {
			return nil, nil
		}
		return t, nil
	case *cell.Builder:
		if t == nil {
			return nil, nil
		}
		cp := t.Copy()
		if trace != nil {
			cp.SetTrace(cell.CombineTraces(cp.Trace(), trace))
		}
		return cp, nil
	case *cell.Slice:
		if t == nil {
			return t, nil
		}
		cp := t.Copy()
		if trace != nil {
			cp.SetTrace(cell.CombineTraces(cp.Trace(), trace))
		}
		return cp, nil
	case tuple.Tuple:
		return bindTupleTrace(t, trace), nil
	case nil:
		return nil, nil
	default:
		c, ok := val.(Continuation)
		if !ok {
			return nil, vmerr.Error(vmerr.CodeTypeCheck, "type check failed: "+reflect.TypeOf(val).String())
		}
		return c.Copy(), nil
	}
}

func (s *Stack) PushBool(val bool) error {
	if val {
		return s.pushStaticInt(stackIntMinusOne)
	}
	return s.pushStaticInt(stackIntZero)
}

func (s *Stack) pushStaticInt(val *big.Int) error {
	s.elems = append(s.elems, val)
	return nil
}

func canonicalStackInt(val *big.Int) *big.Int {
	switch {
	case val.Sign() == 0:
		return stackIntZero
	case val.IsInt64():
		switch val.Int64() {
		case -1:
			return stackIntMinusOne
		case 1:
			return stackIntOne
		}
	}
	return new(big.Int).Set(val)
}

func canonicalOwnedStackInt(val *big.Int) *big.Int {
	switch {
	case val.Sign() == 0:
		return stackIntZero
	case val.IsInt64():
		switch val.Int64() {
		case -1:
			return stackIntMinusOne
		case 1:
			return stackIntOne
		}
	}
	return val
}

func isStaticStackInt(val *big.Int) bool {
	if val == stackIntMinusOne || val == stackIntZero || val == stackIntOne {
		return true
	}
	if val == nil || !val.IsInt64() {
		return false
	}
	static := stackStaticInt(val.Int64())
	return static != nil && val == static
}

// PushHostValue accepts values already encoded with TVM stack types.
func (s *Stack) PushHostValue(val any) error {
	x, ok := val.(*big.Int)
	if ok {
		if x == nil {
			return s.PushAny(nil)
		}
		return s.PushInt(x)
	}
	return s.PushAny(val)
}

func (s *Stack) PushBuilder(val *cell.Builder) error {
	return s.PushAny(val)
}

// PushOwnedBuilder pushes a builder the caller no longer shares elsewhere.
// Unlike PushBuilder, it binds stack trace in-place and skips a defensive copy.
func (s *Stack) PushOwnedBuilder(val *cell.Builder) error {
	if val == nil {
		s.elems = append(s.elems, nil)
		return nil
	}
	if s.trace != nil {
		val.SetTrace(cell.CombineTraces(val.Trace(), s.trace))
	}
	s.elems = append(s.elems, val)
	return nil
}

func (s *Stack) PushSlice(val *cell.Slice) error {
	return s.PushAny(val)
}

// PushOwnedSlice pushes a slice the caller no longer shares elsewhere.
// Unlike PushSlice, it binds stack trace in-place and skips a defensive copy.
func (s *Stack) PushOwnedSlice(val *cell.Slice) error {
	if val == nil {
		s.elems = append(s.elems, val)
		return nil
	}
	if s.trace != nil {
		val.SetTrace(cell.CombineTraces(val.Trace(), s.trace))
	}
	s.elems = append(s.elems, val)
	return nil
}

func (s *Stack) PushCell(val *cell.Cell) error {
	return s.PushAny(val)
}

func (s *Stack) PushContinuation(val Continuation) error {
	return s.PushAny(val)
}

func fitsTVMInt(val *big.Int) bool {
	return val.Cmp(minTVMInt) >= 0 && val.Cmp(maxTVMInt) <= 0
}

func (s *Stack) PushInt(val *big.Int) error {
	if !fitsTVMInt(val) {
		return vmerr.Error(vmerr.CodeIntOverflow)
	}
	return s.pushStaticInt(canonicalStackInt(val))
}

// PushOwnedInt pushes an integer the caller no longer shares elsewhere.
// Unlike PushInt, it keeps non-static values in-place and skips a defensive copy.
func (s *Stack) PushOwnedInt(val *big.Int) error {
	if !fitsTVMInt(val) {
		return vmerr.Error(vmerr.CodeIntOverflow)
	}
	return s.pushStaticInt(canonicalOwnedStackInt(val))
}

func (s *Stack) PushIntQuiet(val *big.Int) error {
	if !fitsTVMInt(val) {
		return s.PushAny(NaN{})
	}
	return s.pushStaticInt(canonicalStackInt(val))
}

// PushOwnedIntQuiet is the quiet variant of PushOwnedInt.
func (s *Stack) PushOwnedIntQuiet(val *big.Int) error {
	if !fitsTVMInt(val) {
		return s.PushAny(NaN{})
	}
	return s.pushStaticInt(canonicalOwnedStackInt(val))
}

func (s *Stack) PushSmallInt(val int64) error {
	if static := stackStaticInt(val); static != nil {
		return s.pushStaticInt(static)
	}
	return s.PushOwnedInt(big.NewInt(val))
}

func (s *Stack) PushAny(val any) error {
	var err error
	val, err = shareStackValue(val, s.trace)
	if err != nil {
		return err
	}

	s.elems = append(s.elems, val)
	return nil
}

// PushOwnedValue pushes a value that was already removed from this VM stack.
func (s *Stack) PushOwnedValue(val any) error {
	if v, ok := val.(*big.Int); ok &&
		(v == stackIntMinusOne || v == stackIntZero || v == stackIntOne) {
		s.elems = append(s.elems, v)
		return nil
	}

	return s.pushOwnedValueChecked(val)
}

func (s *Stack) pushOwnedValueChecked(val any) error {
	switch v := val.(type) {
	case nil, NaN:
	case *big.Int:
		if v == nil {
			val = nil
		} else if !fitsTVMInt(v) {
			return vmerr.Error(vmerr.CodeIntOverflow)
		} else {
			val = canonicalOwnedStackInt(v)
		}
	case *cell.Cell:
		if v == nil {
			val = nil
		}
	case *cell.Builder:
		if v == nil {
			val = nil
		}
	case *cell.Slice:
		// Keep a typed nil slice: its null-slice tag is observable by TVM.
	case tuple.Tuple:
		if v.IsNull() {
			val = nil
		}
	case Continuation:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Ptr && rv.IsNil() {
			return vmerr.Error(vmerr.CodeTypeCheck, "nil continuation")
		}
	default:
		return vmerr.Error(vmerr.CodeTypeCheck, "unsupported stack value")
	}

	s.elems = append(s.elems, val)
	return nil
}

func (s *Stack) SplitTop(top, drop int) (*Stack, error) {
	n := s.Len()
	if top < 0 || drop < 0 || top > n || drop > n-top {
		return nil, vmerr.Error(vmerr.CodeStackUnderflow)
	}

	newStack := newStackWithCap(top)
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
	if num < 0 || len(from.elems) < num {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	fromStart := len(from.elems) - num
	s.elems = append(s.elems, from.elems[fromStart:]...)
	from.elems = from.elems[:fromStart]

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
	if uint(num) > uint(len(s.elems)) {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	s.elems = s.elems[:len(s.elems)-num]
	return nil
}

func (s *Stack) DropMany(num, offs int) error {
	if uint(num) > uint(len(s.elems)) || uint(offs) > uint(len(s.elems)-num) {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
	if num == 0 {
		return nil
	}

	end := len(s.elems)
	copy(s.elems[end-(num+offs):end-num], s.elems[end-offs:end])
	s.elems = s.elems[:end-num]
	return nil
}

func (s *Stack) DropAfter(num int) error {
	if uint(num) > uint(len(s.elems)) {
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

	switch v := e.(type) {
	case NaN:
		return nil, nil
	case *big.Int:
		if isStaticStackInt(v) {
			return new(big.Int).Set(v), nil
		}
		return v, nil
	default:
		return nil, vmerr.Error(vmerr.CodeTypeCheck, "not an integer")
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

// PopIntRead pops an integer for read-only use: shared static instances are
// returned as-is instead of a defensive copy, so the caller must not mutate
// the result. nil result means NaN.
func (s *Stack) PopIntRead() (*big.Int, error) {
	e, err := s.PopAny()
	if err != nil {
		return nil, err
	}

	switch v := e.(type) {
	case NaN:
		return nil, nil
	case *big.Int:
		return v, nil
	default:
		return nil, vmerr.Error(vmerr.CodeTypeCheck, "not an integer")
	}
}

// PopIntFiniteRead is the finite variant of PopIntRead: the caller must not
// mutate the result.
func (s *Stack) PopIntFiniteRead() (*big.Int, error) {
	e, err := s.PopIntRead()
	if err != nil {
		return nil, err
	}
	if e == nil { // nil = non valid (NaN)
		return nil, vmerr.Error(vmerr.CodeIntOverflow)
	}
	return e, nil
}

func (s *Stack) PopBool() (bool, error) {
	e, err := s.PopAny()
	if err != nil {
		return false, err
	}
	switch v := e.(type) {
	case NaN:
		return false, vmerr.Error(vmerr.CodeIntOverflow)
	case *big.Int:
		return v.Sign() != 0, nil
	default:
		return false, vmerr.Error(vmerr.CodeTypeCheck, "not an integer")
	}
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
	if v, ok := e.(*cell.Slice); !ok || v == nil {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	} else {
		return v, nil
	}
}

func (s *Stack) PopTuple() (tuple.Tuple, error) {
	e, err := s.PopAny()
	if err != nil {
		return tuple.Tuple{}, err
	}
	if v, ok := e.(tuple.Tuple); ok && !v.IsNull() {
		return v, nil
	}
	return tuple.Tuple{}, vmerr.Error(vmerr.CodeTypeCheck, "not a tuple")
}

func (s *Stack) PopTupleRange(max int, min ...int) (tuple.Tuple, error) {
	e, err := s.PopAny()
	if err != nil {
		return tuple.Tuple{}, err
	}
	t, ok := e.(tuple.Tuple)
	if !ok || t.IsNull() {
		return tuple.Tuple{}, vmerr.Error(vmerr.CodeTypeCheck, "not a tuple of valid size")
	}

	lower := 0
	if len(min) > 0 {
		lower = min[0]
	}

	if (max >= 0 && t.Len() > max) || t.Len() < lower {
		return tuple.Tuple{}, vmerr.Error(vmerr.CodeTypeCheck, "not a tuple of valid size")
	}

	return t, nil
}

func (s *Stack) PopMaybeTupleRange(max int) (*tuple.Tuple, error) {
	e, err := s.PopAny()
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, nil
	}
	v, ok := e.(tuple.Tuple)
	if !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck, "not a tuple of valid size")
	}
	if max >= 0 && v.Len() > max {
		return nil, vmerr.Error(vmerr.CodeTypeCheck, "not a tuple of valid size")
	}
	return &v, nil
}

func (s *Stack) PushTuple(t tuple.Tuple) error {
	return s.PushAny(t)
}

func (s *Stack) PushMaybeTuple(t *tuple.Tuple) error {
	if t == nil {
		return s.PushAny(nil)
	}
	return s.PushAny(*t)
}

func (s *Stack) PopIntRange(min, max int64) (*big.Int, error) {
	e, err := s.PopInt()
	if err != nil {
		return nil, err
	}

	if e == nil {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}

	if !e.IsInt64() {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}
	val := e.Int64()
	if val < min || val > max {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}
	return e, nil
}

func (s *Stack) PopIntRangeInt64(min, max int64) (int64, error) {
	e, err := s.PopAny()
	if err != nil {
		return 0, err
	}

	switch v := e.(type) {
	case NaN:
		return 0, vmerr.Error(vmerr.CodeRangeCheck)
	case *big.Int:
		if !v.IsInt64() {
			return 0, vmerr.Error(vmerr.CodeRangeCheck)
		}
		val := v.Int64()
		if val < min || val > max {
			return 0, vmerr.Error(vmerr.CodeRangeCheck)
		}
		return val, nil
	default:
		return 0, vmerr.Error(vmerr.CodeTypeCheck, "not an integer")
	}
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

	var res strings.Builder
	for i := len(s.elems) - 1; i >= 0; i-- {
		typ := "???"
		val := "???"
		switch x := s.elems[s.index(i)].(type) {
		case nil:
			typ = "nil"
			val = "nil"
		case NaN:
			typ = "nan"
			val = "NaN"
		case *big.Int:
			typ = "int"
			val = x.String()
		case *cell.Slice:
			typ = "slice"
			if x == nil {
				val = "null"
			} else {
				val = x.WithoutTrace().MustToCell().Dump()
			}
		case *cell.Builder:
			typ = "builder"
			val = x.WithoutTrace().EndCell().Dump()
		case *cell.Cell:
			typ = "cell"
			val = x.WithoutTrace().Dump()
		}

		fmt.Fprintf(&res, "s%d = %s [%s]\n", i, val, typ)
	}
	return res.String()
}

func (s *Stack) Copy() *Stack {
	c := &Stack{
		elems: make([]any, len(s.elems)),
		trace: s.trace,
	}
	for i, elem := range s.elems {
		cloned, err := shareStackValue(elem, c.trace)
		if err != nil {
			panic(err)
		}
		c.elems[i] = cloned
	}
	return c
}

func (s *Stack) Restore(src *Stack) {
	s.elems = src.elems
	s.trace = src.trace
}

func (s *Stack) Snapshot() *Stack {
	return &Stack{
		elems: append([]any(nil), s.elems...),
		trace: s.trace,
	}
}

func (s *Stack) FromTop(offset int) (int, error) {
	stackLen := len(s.elems)
	if uint(offset) >= uint(stackLen) {
		return 0, vmerr.Error(vmerr.CodeStackUnderflow)
	}
	return stackLen - 1 - offset, nil
}

func (s *Stack) Rotate(from, to int) error {
	stackLen := len(s.elems)
	if from < 0 || to < 0 || from+to <= 0 || from+to > stackLen {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	if err := s.Reverse(stackLen, from+to); err != nil {
		return err
	}
	if err := s.Reverse(from+to, 0); err != nil {
		return err
	}

	return s.Reverse(stackLen, 0)
}

// Reverse reverses a half-open stack range using the same offsets as C++ from_top.
func (s *Stack) Reverse(from, to int) error {
	stackLen := len(s.elems)
	if from < 0 || to < 0 || from > stackLen || to > stackLen || from < to {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	for l, r := stackLen-from, stackLen-to-1; l < r; l, r = l+1, r-1 {
		s.elems[l], s.elems[r] = s.elems[r], s.elems[l]
	}

	return nil
}
