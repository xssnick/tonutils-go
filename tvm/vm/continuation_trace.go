package vm

import (
	"math/big"
	"reflect"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

// continuationTraceCopier removes an execution-local trace at a VM ownership
// boundary. Its maps preserve sharing in tuple DAGs and continuation graphs,
// including code, captured stacks, and saved registers.
type continuationTraceCopier struct {
	trace  *cell.Trace
	copied map[Continuation]Continuation
	tuples map[tuple.Tuple]tuple.Tuple
}

func (c *continuationTraceCopier) continuation(cont Continuation) Continuation {
	if cont == nil {
		return nil
	}
	if copied, ok := c.lookup(cont); ok {
		return copied
	}

	switch src := cont.(type) {
	case *QuitContinuation:
		dst := &QuitContinuation{ExitCode: src.ExitCode}
		c.remember(src, dst)
		return dst
	case *ExcQuitContinuation:
		c.remember(src, src)
		return src
	case *OrdinaryContinuation:
		dst := &OrdinaryContinuation{}
		c.remember(src, dst)
		dst.Data = c.controlData(src.Data)
		dst.Code = c.slice(src.Code)
		return dst
	case *ArgExtContinuation:
		dst := &ArgExtContinuation{}
		c.remember(src, dst)
		dst.Data = c.controlData(src.Data)
		dst.Ext = c.continuation(src.Ext)
		return dst
	case *PushIntContinuation:
		dst := &PushIntContinuation{Int: src.Int}
		c.remember(src, dst)
		dst.Next = c.continuation(src.Next)
		return dst
	case *RepeatContinuation:
		dst := &RepeatContinuation{Count: src.Count}
		c.remember(src, dst)
		dst.Body = c.continuation(src.Body)
		dst.After = c.continuation(src.After)
		return dst
	case *AgainContinuation:
		dst := &AgainContinuation{}
		c.remember(src, dst)
		dst.Body = c.continuation(src.Body)
		return dst
	case *WhileContinuation:
		dst := &WhileContinuation{CheckCond: src.CheckCond}
		c.remember(src, dst)
		dst.Body = c.continuation(src.Body)
		dst.Cond = c.continuation(src.Cond)
		dst.After = c.continuation(src.After)
		return dst
	case *UntilContinuation:
		dst := &UntilContinuation{}
		c.remember(src, dst)
		dst.Body = c.continuation(src.Body)
		dst.After = c.continuation(src.After)
		return dst
	default:
		// Custom continuations can expose captured stack/register data through
		// GetControlData, but their private fields remain owned by their Copy.
		dst := cont.Copy()
		c.remember(cont, dst)
		if data := dst.GetControlData(); data != nil {
			*data = c.controlData(*data)
		}
		return dst
	}
}

func (c *continuationTraceCopier) lookup(cont Continuation) (Continuation, bool) {
	typ := reflect.TypeOf(cont)
	if typ == nil || !typ.Comparable() || c.copied == nil {
		return nil, false
	}
	copied, ok := c.copied[cont]
	return copied, ok
}

func (c *continuationTraceCopier) remember(src, dst Continuation) {
	typ := reflect.TypeOf(src)
	if typ == nil || !typ.Comparable() {
		return
	}
	if c.copied == nil {
		c.copied = make(map[Continuation]Continuation)
	}
	c.copied[src] = dst
}

func (c *continuationTraceCopier) controlData(data ControlData) ControlData {
	return ControlData{
		Save:    c.register(data.Save),
		Stack:   c.stack(data.Stack),
		NumArgs: data.NumArgs,
		CP:      data.CP,
	}
}

func (c *continuationTraceCopier) register(reg Register) Register {
	var dst Register
	for i, cont := range reg.C {
		dst.C[i] = c.continuation(cont)
	}
	for i, cl := range reg.D {
		if cl != nil {
			dst.D[i] = cl.WithTrace(cl.Trace().WithoutTrace(c.trace))
		}
	}
	dst.C7 = c.tuple(reg.C7)
	return dst
}

func (c *continuationTraceCopier) stack(stack *Stack) *Stack {
	if stack == nil {
		return nil
	}

	dst := &Stack{
		elems: make([]any, len(stack.elems)),
		trace: stack.trace.WithoutTrace(c.trace),
	}
	for i, val := range stack.elems {
		if n, ok := val.(*big.Int); ok && n != nil {
			// Unlike tuple reads, PopInt transfers a mutable integer to the
			// caller. Captured stacks must own their integer leaves.
			dst.elems[i] = new(big.Int).Set(n)
		} else {
			dst.elems[i] = c.value(val)
		}
	}
	return dst
}

func (c *continuationTraceCopier) tuple(t tuple.Tuple) tuple.Tuple {
	if t.IsNull() {
		return t
	}
	if copied, ok := c.tuples[t]; ok {
		return copied
	}

	var vals []any
	for i := 0; i < t.Len(); i++ {
		val, err := t.RawIndex(i)
		if err != nil {
			panic(err)
		}
		next := c.value(val)
		if vals == nil && sameStackValue(next, val) {
			continue
		}
		if vals == nil {
			vals = make([]any, t.Len())
			copyTuplePrefix(vals, t, i)
		}
		vals[i] = next
	}

	var dst tuple.Tuple
	if vals == nil {
		dst = t.WithBindingID(nil)
	} else {
		dst = tuple.NewTupleOwned(vals)
	}
	if c.tuples == nil {
		c.tuples = make(map[tuple.Tuple]tuple.Tuple)
	}
	c.tuples[t] = dst
	return dst
}

func (c *continuationTraceCopier) value(val any) any {
	switch v := val.(type) {
	case *cell.Cell:
		if v == nil {
			return v
		}
		return v.WithTrace(v.Trace().WithoutTrace(c.trace))
	case *cell.Slice:
		return c.slice(v)
	case *cell.Builder:
		if v == nil {
			return v
		}
		return v.Copy().SetTrace(v.Trace().WithoutTrace(c.trace))
	case tuple.Tuple:
		return c.tuple(v)
	case Continuation:
		return c.continuation(v)
	default:
		return val
	}
}

func (c *continuationTraceCopier) slice(sl *cell.Slice) *cell.Slice {
	if sl == nil {
		return nil
	}
	return sl.Copy().SetTrace(sl.Trace().WithoutTrace(c.trace))
}
