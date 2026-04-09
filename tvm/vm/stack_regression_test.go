package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func pushInts(t *testing.T, s *Stack, vals ...int64) {
	t.Helper()

	for _, v := range vals {
		if err := s.PushInt(big.NewInt(v)); err != nil {
			t.Fatalf("push %d: %v", v, err)
		}
	}
}

func assertPopInts(t *testing.T, s *Stack, vals ...int64) {
	t.Helper()

	for _, want := range vals {
		got, err := s.PopInt()
		if err != nil {
			t.Fatalf("pop %d: %v", want, err)
		}
		if got.Int64() != want {
			t.Fatalf("want %d, got %d", want, got.Int64())
		}
	}
}

func TestStackMoveFromMovesTopElements(t *testing.T) {
	from := NewStack()
	pushInts(t, from, 1, 2, 3, 4)

	to := NewStack()
	if err := to.MoveFrom(from, 2); err != nil {
		t.Fatal(err)
	}

	assertPopInts(t, to, 4, 3)
	assertPopInts(t, from, 2, 1)
}

func TestStackSplitTopAllowsWholeStack(t *testing.T) {
	s := NewStack()
	pushInts(t, s, 1, 2, 3)

	top, err := s.SplitTop(3, 0)
	if err != nil {
		t.Fatal(err)
	}

	if s.Len() != 0 {
		t.Fatalf("source stack should be empty, got len=%d", s.Len())
	}

	assertPopInts(t, top, 3, 2, 1)
}

func TestStackSplitTopTakesTopAndDropsNext(t *testing.T) {
	s := NewStack()
	pushInts(t, s, 1, 2, 3, 4, 5)

	top, err := s.SplitTop(2, 1)
	if err != nil {
		t.Fatal(err)
	}

	assertPopInts(t, top, 5, 4)
	assertPopInts(t, s, 2, 1)
}

func TestStackSplitTopRejectsInvalidArgs(t *testing.T) {
	s := NewStack()
	pushInts(t, s, 1, 2, 3)

	if _, err := s.SplitTop(4, 0); err == nil {
		t.Fatal("expected error for top larger than stack")
	}

	if _, err := s.SplitTop(2, 2); err == nil {
		t.Fatal("expected error for invalid drop size")
	}
}

func TestStackPushIntSupportsTVMSigned257BitRange(t *testing.T) {
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	min := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))
	posOverflow := new(big.Int).Add(new(big.Int).Set(max), big.NewInt(1))
	negOverflow := new(big.Int).Sub(new(big.Int).Set(min), big.NewInt(1))

	s := NewStack()
	if err := s.PushInt(max); err != nil {
		t.Fatalf("max signed 257-bit integer should fit: %v", err)
	}
	if err := s.PushInt(min); err != nil {
		t.Fatalf("min signed 257-bit integer should fit: %v", err)
	}

	gotMin, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}
	if gotMin.Cmp(min) != 0 {
		t.Fatalf("want min %s, got %s", min.String(), gotMin.String())
	}

	gotMax, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}
	if gotMax.Cmp(max) != 0 {
		t.Fatalf("want max %s, got %s", max.String(), gotMax.String())
	}

	if err := s.PushInt(posOverflow); err == nil {
		t.Fatal("expected positive overflow to fail")
	}
	if err := s.PushInt(negOverflow); err == nil {
		t.Fatal("expected negative overflow to fail")
	}
}

func TestStackPushIntQuietKeepsNaNDistinct(t *testing.T) {
	s := NewStack()
	overflow := new(big.Int).Lsh(big.NewInt(1), 257)

	if err := s.PushIntQuiet(overflow); err != nil {
		t.Fatalf("push quiet overflow: %v", err)
	}

	val, err := s.Get(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := val.(NaN); !ok {
		t.Fatalf("expected NaN on stack, got %T", val)
	}

	if _, err = s.PopIntFinite(); err == nil {
		t.Fatal("expected PopIntFinite to reject NaN")
	} else {
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeIntOverflow {
			t.Fatalf("expected int overflow, got %v", err)
		}
	}
}

func TestStateCallArgsPassesTopElements(t *testing.T) {
	s := NewStack()
	pushInts(t, s, 1, 2, 3, 4)

	state := &State{
		CurrentCode: cell.BeginCell().EndCell().BeginParse(),
		Stack:       s,
	}

	cont := &OrdinaryContinuation{
		Data: ControlData{
			NumArgs: 2,
			CP:      CP,
		},
		Code: cell.BeginCell().EndCell().BeginParse(),
	}

	if err := state.CallArgs(cont, 2, -1); err != nil {
		t.Fatal(err)
	}

	assertPopInts(t, state.Stack, 4, 3)

	ret, ok := state.Reg.C[0].(*OrdinaryContinuation)
	if !ok {
		t.Fatalf("expected return continuation, got %T", state.Reg.C[0])
	}

	assertPopInts(t, ret.Data.Stack, 2, 1)
}

func TestStateCallArgsWithCapturedStackPreservesClosureStack(t *testing.T) {
	s := NewStack()
	pushInts(t, s, 1, 2, 3, 4)

	closureStack := NewStack()
	pushInts(t, closureStack, 8, 9)

	state := &State{
		CurrentCode: cell.BeginCell().EndCell().BeginParse(),
		Stack:       s,
	}

	cont := &OrdinaryContinuation{
		Data: ControlData{
			Stack:   closureStack,
			NumArgs: 2,
			CP:      CP,
		},
		Code: cell.BeginCell().EndCell().BeginParse(),
	}

	if err := state.CallArgs(cont, 3, -1); err != nil {
		t.Fatal(err)
	}

	assertPopInts(t, state.Stack, 4, 3, 9, 8)

	ret, ok := state.Reg.C[0].(*OrdinaryContinuation)
	if !ok {
		t.Fatalf("expected return continuation, got %T", state.Reg.C[0])
	}

	assertPopInts(t, ret.Data.Stack, 1)
}

func TestStateJumpArgsWithCapturedStackUsesClosureStack(t *testing.T) {
	s := NewStack()
	pushInts(t, s, 1, 2, 3, 4)

	closureStack := NewStack()
	pushInts(t, closureStack, 8, 9)

	state := &State{
		CurrentCode: cell.BeginCell().EndCell().BeginParse(),
		Stack:       s,
	}

	cont := &OrdinaryContinuation{
		Data: ControlData{
			Stack:   closureStack,
			NumArgs: 2,
			CP:      CP,
		},
		Code: cell.BeginCell().EndCell().BeginParse(),
	}

	if err := state.JumpArgs(cont, 2); err != nil {
		t.Fatal(err)
	}

	assertPopInts(t, state.Stack, 4, 3, 9, 8)
}

func TestStackCopyClonesMutableValues(t *testing.T) {
	src := NewStack()

	intVal := big.NewInt(7)
	sliceVal := cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()).EndCell().BeginParse()
	builderVal := cell.BeginCell().MustStoreUInt(0xEF, 8)
	tupleVal := tuple.NewTupleSized(1)
	if err := tupleVal.Set(0, cell.BeginCell().MustStoreUInt(0x12, 8).MustStoreRef(cell.BeginCell().EndCell()).EndCell().BeginParse()); err != nil {
		t.Fatal(err)
	}

	if err := src.PushAny(intVal); err != nil {
		t.Fatal(err)
	}
	if err := src.PushAny(sliceVal); err != nil {
		t.Fatal(err)
	}
	if err := src.PushAny(builderVal); err != nil {
		t.Fatal(err)
	}
	if err := src.PushAny(tupleVal); err != nil {
		t.Fatal(err)
	}

	cp := src.Copy()

	origTupleAny, err := src.Get(0)
	if err != nil {
		t.Fatal(err)
	}
	copiedTupleAny, err := cp.Get(0)
	if err != nil {
		t.Fatal(err)
	}
	origTuple := origTupleAny.(tuple.Tuple)
	copiedTuple := copiedTupleAny.(tuple.Tuple)

	origBuilder, err := src.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	copiedBuilder, err := cp.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	origSlice, err := src.Get(2)
	if err != nil {
		t.Fatal(err)
	}
	copiedSlice, err := cp.Get(2)
	if err != nil {
		t.Fatal(err)
	}
	origInt, err := src.Get(3)
	if err != nil {
		t.Fatal(err)
	}
	copiedInt, err := cp.Get(3)
	if err != nil {
		t.Fatal(err)
	}

	if origBuilder == copiedBuilder {
		t.Fatal("builder should be cloned")
	}
	if origSlice == copiedSlice {
		t.Fatal("slice should be cloned")
	}
	if origInt == copiedInt {
		t.Fatal("big.Int should be cloned")
	}

	origNested, err := origTuple.Index(0)
	if err != nil {
		t.Fatal(err)
	}
	copiedNested, err := copiedTuple.Index(0)
	if err != nil {
		t.Fatal(err)
	}
	if origNested == copiedNested {
		t.Fatal("nested tuple slice should be cloned")
	}

	if _, err = copiedSlice.(*cell.Slice).LoadUInt(8); err != nil {
		t.Fatal(err)
	}
	if got := origSlice.(*cell.Slice).BitsLeft(); got != 8 {
		t.Fatalf("original slice should stay intact, got bits left %d", got)
	}

	if err = copiedBuilder.(*cell.Builder).StoreUInt(1, 1); err != nil {
		t.Fatal(err)
	}
	if got := origBuilder.(*cell.Builder).BitsUsed(); got != 8 {
		t.Fatalf("original builder should stay intact, got bits used %d", got)
	}
}

func TestStateUpdateC7DoesNotAliasOriginalTuple(t *testing.T) {
	inner := tuple.NewTupleSized(1)
	if err := inner.Set(0, cell.BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(cell.BeginCell().EndCell()).EndCell().BeginParse()); err != nil {
		t.Fatal(err)
	}

	c7 := tuple.NewTupleSized(2)
	params := tuple.NewTupleSized(1)
	if err := c7.Set(0, params); err != nil {
		t.Fatal(err)
	}
	if err := c7.Set(1, inner); err != nil {
		t.Fatal(err)
	}

	state := NewExecutionState(DefaultGlobalVersion, NewGas(), cell.BeginCell().EndCell(), c7, NewStack())
	state.InitForExecution()

	if err := state.UpdateC7(func(t tuple.Tuple) (tuple.Tuple, error) {
		v, err := t.Index(1)
		if err != nil {
			return tuple.Tuple{}, err
		}
		nested := v.(tuple.Tuple)
		if err = nested.Set(0, cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell().BeginParse()); err != nil {
			return tuple.Tuple{}, err
		}
		if err = t.Set(1, nested); err != nil {
			return tuple.Tuple{}, err
		}
		return t, nil
	}); err != nil {
		t.Fatal(err)
	}

	origAny, err := c7.Index(1)
	if err != nil {
		t.Fatal(err)
	}
	origNested := origAny.(tuple.Tuple)
	origSliceAny, err := origNested.Index(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := origSliceAny.(*cell.Slice).MustLoadUInt(8); got != 0xAA {
		t.Fatalf("original tuple should stay intact, got %x", got)
	}

	updatedAny, err := state.Reg.C7.Index(1)
	if err != nil {
		t.Fatal(err)
	}
	updatedNested := updatedAny.(tuple.Tuple)
	updatedSliceAny, err := updatedNested.Index(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := updatedSliceAny.(*cell.Slice).MustLoadUInt(8); got != 0xBB {
		t.Fatalf("updated tuple should contain new value, got %x", got)
	}
}

func TestStackCopyHandlesDeepNestedTupleTree(t *testing.T) {
	const depth = 10000

	deep := tuple.NewTupleSized(1)
	if err := (&deep).Set(0, big.NewInt(1)); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < depth; i++ {
		next := tuple.NewTupleSized(1)
		if err := (&next).Set(0, deep.Copy()); err != nil {
			t.Fatal(err)
		}
		deep = next
	}

	src := NewStack()
	if err := src.PushTuple(deep); err != nil {
		t.Fatal(err)
	}

	cp := src.Copy()
	if cp.Len() != 1 {
		t.Fatalf("expected copied stack len 1, got %d", cp.Len())
	}

	gotAny, err := cp.Get(0)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := gotAny.(tuple.Tuple)
	if !ok {
		t.Fatalf("expected tuple on copied stack, got %T", gotAny)
	}

	cur := got
	for i := 0; i < depth; i++ {
		nextAny, err := cur.Index(0)
		if err != nil {
			t.Fatalf("read nested tuple %d: %v", i, err)
		}
		next, ok := nextAny.(tuple.Tuple)
		if !ok {
			t.Fatalf("expected nested tuple at depth %d, got %T", i, nextAny)
		}
		cur = next
	}

	leaf, err := cur.Index(0)
	if err != nil {
		t.Fatal(err)
	}
	if leaf.(*big.Int).Int64() != 1 {
		t.Fatalf("expected final leaf 1, got %v", leaf)
	}
}

func TestStackCopyHandlesDeepContinuationChain(t *testing.T) {
	const depth = 10000

	var cont Continuation = &QuitContinuation{ExitCode: 0}
	for i := 0; i < depth; i++ {
		cont = &ArgExtContinuation{
			Data: ControlData{
				NumArgs: ControlDataAllArgs,
				CP:      CP,
			},
			Ext: cont,
		}
	}

	src := NewStack()
	if err := src.PushContinuation(cont); err != nil {
		t.Fatal(err)
	}

	cp := src.Copy()
	if cp.Len() != 1 {
		t.Fatalf("expected copied stack len 1, got %d", cp.Len())
	}

	got, err := cp.PopContinuation()
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < depth; i++ {
		argExt, ok := got.(*ArgExtContinuation)
		if !ok {
			t.Fatalf("expected arg-ext continuation at depth %d, got %T", i, got)
		}
		got = argExt.Ext
	}

	if _, ok := got.(*QuitContinuation); !ok {
		t.Fatalf("expected quit continuation at tail, got %T", got)
	}
}
