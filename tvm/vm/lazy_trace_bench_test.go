package vm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestStackTraceBindingKeepsTupleIsolation(t *testing.T) {
	original := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().MustBeginParse()
	stack := NewStack()
	if err := stack.PushTuple(tuple.NewTupleValue(original)); err != nil {
		t.Fatal(err)
	}

	trace := cell.NewTrace(cell.TraceHooks{OnLoad: func(*cell.Cell) {}})
	stack.SetTrace(trace)

	stored := stack.elems[0].(tuple.Tuple)
	storedAny, err := stored.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	storedSlice := storedAny.(*cell.Slice)
	if storedSlice == original {
		t.Fatal("tuple cursor was not isolated on stack entry")
	}
	if storedSlice.Trace() != trace {
		t.Fatal("stack SetTrace did not attach the gas trace")
	}

	popped, err := stack.PopTuple()
	if err != nil {
		t.Fatal(err)
	}
	poppedAny, err := popped.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	if poppedAny.(*cell.Slice).Trace() != trace {
		t.Fatal("popped stack value lost the gas trace")
	}
}

func TestStackTraceBindingPersistsNestedTupleMarkers(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().MustBeginParse()
	stack := NewStack()
	if err := stack.PushTuple(tuple.NewTupleValue(tuple.NewTupleValue(leaf))); err != nil {
		t.Fatal(err)
	}

	trace := cell.NewTrace(cell.TraceHooks{OnLoad: func(*cell.Cell) {}})
	stack.SetTrace(trace)

	firstAny, err := stack.Get(0)
	if err != nil {
		t.Fatal(err)
	}
	first := firstAny.(tuple.Tuple)
	innerAny, err := first.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	inner := innerAny.(tuple.Tuple)
	if !inner.HasBindingID(trace) {
		t.Fatal("nested tuple binding marker was not persisted")
	}
	leafAny, err := inner.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	if leafAny.(*cell.Slice).Trace() != trace {
		t.Fatal("nested tuple leaf did not receive the stack trace")
	}

	secondAny, err := stack.Get(0)
	if err != nil {
		t.Fatal(err)
	}
	second := secondAny.(tuple.Tuple)
	secondInnerAny, err := second.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	if secondInnerAny.(tuple.Tuple) != inner {
		t.Fatal("repeated stack read rebuilt an already-bound nested tuple")
	}
}

func TestStackMoveFromCarriesSourceTrace(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	root := cell.BeginCell().MustStoreRef(leaf).EndCell()
	source := NewStack()
	if err := source.PushSlice(root.MustBeginParse()); err != nil {
		t.Fatal(err)
	}

	loads := 0
	trace := cell.NewTrace(cell.TraceHooks{
		OnChild: func(int) *cell.Trace {
			return cell.NewTrace(cell.TraceHooks{OnLoad: func(*cell.Cell) { loads++ }})
		},
	})
	source.SetTrace(trace)

	// Captured continuation stacks can intentionally have no stack-level
	// trace. The moved value must still preserve the source execution trace.
	captured := NewStack()
	if err := captured.MoveFrom(source, 1); err != nil {
		t.Fatal(err)
	}
	cloned := captured.Copy()
	moved, err := cloned.PopSlice()
	if err != nil {
		t.Fatal(err)
	}
	if moved.Trace() != trace {
		t.Fatal("moved slice lost the source stack trace")
	}
	if _, err = moved.LoadRef(); err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("child loads = %d, want 1", loads)
	}
}

func TestC7TraceBindingIsLazyAndKeepsExecutionSnapshot(t *testing.T) {
	original := cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell().MustBeginParse()
	params := tuple.NewTupleValue(original)
	state := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(1_000_000), nil, tuple.NewTupleValue(params), NewStack())
	state.InitForExecution()

	if _, err := original.LoadUInt(1); err != nil {
		t.Fatal(err)
	}
	paramsAny, err := state.Reg.C7.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	storedParams := paramsAny.(tuple.Tuple)
	storedAny, err := storedParams.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	stored := storedAny.(*cell.Slice)
	if stored.BitsLeft() != 8 {
		t.Fatalf("c7 snapshot followed caller mutation: bits=%d", stored.BitsLeft())
	}
	if stored.Trace() != nil {
		t.Fatal("InitForExecution eagerly attached the c7 trace")
	}

	gotAny, err := state.GetParam(0)
	if err != nil {
		t.Fatal(err)
	}
	got := gotAny.(*cell.Slice)
	if got.Trace() != state.Cells.Trace() {
		t.Fatal("GetParam did not attach the gas trace")
	}
	if stored.Trace() != nil {
		t.Fatal("GetParam mutated the stored c7 cursor")
	}
}

func TestC7AlreadyBoundOwnedTupleSkipsSnapshot(t *testing.T) {
	state := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(1_000_000), nil, tuple.Tuple{}, NewStack())
	trace := state.Cells.Trace()
	owned := cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell().MustBeginParse().SetTrace(trace)
	bound := tuple.NewTupleOwnedBound([]any{owned}, trace)
	state.Reg.C7 = bound

	state.InitForExecution()
	stored, err := state.Reg.C7.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	if stored.(*cell.Slice) != owned {
		t.Fatal("already-bound owned c7 tuple was snapshotted again")
	}

	if err = state.SetC7(bound); err != nil {
		t.Fatal(err)
	}
	stored, err = state.Reg.C7.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	if stored.(*cell.Slice) != owned {
		t.Fatal("SetC7 snapshotted an already-bound owned tuple")
	}
}

func scalarTraceBenchmarkTuple() tuple.Tuple {
	leaf := tuple.NewTupleValue(big.NewInt(1), big.NewInt(2), nil)
	return tuple.NewTupleValue(
		tuple.NewTupleValue(leaf, leaf, leaf),
		tuple.NewTupleValue(leaf, leaf, leaf),
		tuple.NewTupleValue(leaf, leaf, leaf),
	)
}

func BenchmarkStackSetTraceScalarTuple(b *testing.B) {
	value := scalarTraceBenchmarkTuple()
	trace := cell.NewTrace(cell.TraceHooks{OnLoad: func(*cell.Cell) {}})

	b.ReportAllocs()
	for b.Loop() {
		stack := NewStack()
		if err := stack.PushTuple(value); err != nil {
			b.Fatal(err)
		}
		stack.SetTrace(trace)
	}
}

func BenchmarkInitForExecutionScalarC7(b *testing.B) {
	c7 := scalarTraceBenchmarkTuple()

	b.ReportAllocs()
	for b.Loop() {
		state := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(1_000_000), nil, c7, NewStack())
		state.InitForExecution()
	}
}
