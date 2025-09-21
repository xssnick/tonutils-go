package tuple

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func newState() *vm.State {
	return &vm.State{Stack: vm.NewStack()}
}

func pushInts(t *testing.T, state *vm.State, values ...int64) {
	t.Helper()
	for _, v := range values {
		if err := state.Stack.PushInt(big.NewInt(v)); err != nil {
			t.Fatalf("failed to push int: %v", err)
		}
	}
}

func popInt(t *testing.T, state *vm.State) int64 {
	t.Helper()
	val, err := state.Stack.PopInt()
	if err != nil {
		t.Fatalf("failed to pop int: %v", err)
	}
	return val.Int64()
}

func popAny(t *testing.T, state *vm.State) any {
	t.Helper()
	val, err := state.Stack.PopAny()
	if err != nil {
		t.Fatalf("failed to pop value: %v", err)
	}
	return val
}

func TestPushNullAndIsNull(t *testing.T) {
	state := newState()

	if err := PUSHNULL().Interpret(state); err != nil {
		t.Fatalf("PUSHNULL failed: %v", err)
	}
	if val := popAny(t, state); val != nil {
		t.Fatalf("expected nil on stack, got %v", val)
	}

	if err := state.Stack.PushAny(nil); err != nil {
		t.Fatalf("failed to push nil: %v", err)
	}
	if err := ISNULL().Interpret(state); err != nil {
		t.Fatalf("ISNULL failed: %v", err)
	}
	boolVal, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("failed to pop bool: %v", err)
	}
	if !boolVal {
		t.Fatalf("expected true from ISNULL on nil")
	}

	if err := state.Stack.PushInt(big.NewInt(7)); err != nil {
		t.Fatalf("failed to push int: %v", err)
	}
	if err := ISNULL().Interpret(state); err != nil {
		t.Fatalf("ISNULL failed: %v", err)
	}
	boolVal, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("failed to pop bool: %v", err)
	}
	if boolVal {
		t.Fatalf("expected false from ISNULL on non-nil")
	}
}

func TestTupleCreationAndIndexing(t *testing.T) {
	state := newState()

	pushInts(t, state, 1, 2, 3)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}

	val, err := state.Stack.Get(0)
	if err != nil {
		t.Fatalf("failed to get tuple: %v", err)
	}
	tup, ok := val.(tuplepkg.Tuple)
	if !ok {
		t.Fatalf("expected tuple on stack, got %T", val)
	}
	for i, expected := range []int64{1, 2, 3} {
		item, err := tup.Index(i)
		if err != nil {
			t.Fatalf("tuple index failed: %v", err)
		}
		got := item.(*big.Int).Int64()
		if got != expected {
			t.Fatalf("tuple[%d] = %d, want %d", i, got, expected)
		}
	}

	if err := INDEX(1).Interpret(state); err != nil {
		t.Fatalf("INDEX failed: %v", err)
	}
	if got := popInt(t, state); got != 2 {
		t.Fatalf("INDEX returned %d, want 2", got)
	}

	pushInts(t, state, 4, 5, 6)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := INDEXQ(5).Interpret(state); err != nil {
		t.Fatalf("INDEXQ failed: %v", err)
	}
	if val := popAny(t, state); val != nil {
		t.Fatalf("INDEXQ out of range expected nil, got %v", val)
	}

	pushInts(t, state, 7, 8, 9)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := INDEXQ(2).Interpret(state); err != nil {
		t.Fatalf("INDEXQ failed: %v", err)
	}
	if got := popInt(t, state); got != 9 {
		t.Fatalf("INDEXQ returned %d, want 9", got)
	}

	pushInts(t, state, 11, 12, 13)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("failed to push index: %v", err)
	}
	if err := INDEXVAR().Interpret(state); err != nil {
		t.Fatalf("INDEXVAR failed: %v", err)
	}
	if got := popInt(t, state); got != 12 {
		t.Fatalf("INDEXVAR returned %d, want 12", got)
	}

	pushInts(t, state, 21, 22, 23)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(5)); err != nil {
		t.Fatalf("failed to push index: %v", err)
	}
	if err := INDEXVARQ().Interpret(state); err != nil {
		t.Fatalf("INDEXVARQ failed: %v", err)
	}
	if val := popAny(t, state); val != nil {
		t.Fatalf("INDEXVARQ out of range expected nil, got %v", val)
	}
}

func TestTupleSetOperations(t *testing.T) {
	state := newState()

	pushInts(t, state, 1, 2, 3)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
		t.Fatalf("failed to push value: %v", err)
	}
	if err := SETINDEX(1).Interpret(state); err != nil {
		t.Fatalf("SETINDEX failed: %v", err)
	}
	updated := popAny(t, state)
	tup, ok := updated.(tuplepkg.Tuple)
	if !ok {
		t.Fatalf("expected tuple result, got %T", updated)
	}
	item, err := tup.Index(1)
	if err != nil {
		t.Fatalf("tuple index failed: %v", err)
	}
	if got := item.(*big.Int).Int64(); got != 99 {
		t.Fatalf("SETINDEX produced %d, want 99", got)
	}

	pushInts(t, state, 4, 5, 6)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(77)); err != nil {
		t.Fatalf("failed to push value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push index: %v", err)
	}
	if err := SETINDEXVAR().Interpret(state); err != nil {
		t.Fatalf("SETINDEXVAR failed: %v", err)
	}
	updated = popAny(t, state)
	tup, ok = updated.(tuplepkg.Tuple)
	if !ok {
		t.Fatalf("expected tuple result, got %T", updated)
	}
	item, err = tup.Index(2)
	if err != nil {
		t.Fatalf("tuple index failed: %v", err)
	}
	if got := item.(*big.Int).Int64(); got != 77 {
		t.Fatalf("SETINDEXVAR produced %d, want 77", got)
	}

	pushInts(t, state, 8)
	if err := TUPLE(1).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(55)); err != nil {
		t.Fatalf("failed to push value: %v", err)
	}
	if err := SETINDEXQ(3).Interpret(state); err != nil {
		t.Fatalf("SETINDEXQ failed: %v", err)
	}
	updated = popAny(t, state)
	tup, ok = updated.(tuplepkg.Tuple)
	if !ok {
		t.Fatalf("expected tuple result, got %T", updated)
	}
	if tup.Len() != 4 {
		t.Fatalf("expected tuple length 4, got %d", tup.Len())
	}
	item, err = tup.Index(3)
	if err != nil {
		t.Fatalf("tuple index failed: %v", err)
	}
	if got := item.(*big.Int).Int64(); got != 55 {
		t.Fatalf("SETINDEXQ produced %d, want 55", got)
	}

	if err := state.Stack.PushAny(nil); err != nil {
		t.Fatalf("failed to push nil tuple: %v", err)
	}
	if err := state.Stack.PushAny(nil); err != nil {
		t.Fatalf("failed to push nil value: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("failed to push index: %v", err)
	}
	if err := SETINDEXVARQ().Interpret(state); err != nil {
		t.Fatalf("SETINDEXVARQ failed: %v", err)
	}
	if val := popAny(t, state); val != nil {
		t.Fatalf("SETINDEXVARQ with nil inputs expected nil, got %v", val)
	}
}

func TestTupleUnpackAndExplode(t *testing.T) {
	state := newState()

	pushInts(t, state, 1, 2, 3)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := UNTUPLE(3).Interpret(state); err != nil {
		t.Fatalf("UNTUPLE failed: %v", err)
	}
	if got := popInt(t, state); got != 3 {
		t.Fatalf("UNTUPLE expected last element 3, got %d", got)
	}
	if got := popInt(t, state); got != 2 {
		t.Fatalf("UNTUPLE expected second element 2, got %d", got)
	}
	if got := popInt(t, state); got != 1 {
		t.Fatalf("UNTUPLE expected first element 1, got %d", got)
	}

	pushInts(t, state, 4, 5, 6)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := UNPACKFIRST(2).Interpret(state); err != nil {
		t.Fatalf("UNPACKFIRST failed: %v", err)
	}
	if got := popInt(t, state); got != 5 {
		t.Fatalf("UNPACKFIRST expected second element 5, got %d", got)
	}
	if got := popInt(t, state); got != 4 {
		t.Fatalf("UNPACKFIRST expected first element 4, got %d", got)
	}

	pushInts(t, state, 7, 8)
	if err := TUPLE(2).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := EXPLODE(2).Interpret(state); err != nil {
		t.Fatalf("EXPLODE failed: %v", err)
	}
	if got := popInt(t, state); got != 2 {
		t.Fatalf("EXPLODE length expected 2, got %d", got)
	}
	if got := popInt(t, state); got != 8 {
		t.Fatalf("EXPLODE expected second element 8, got %d", got)
	}
	if got := popInt(t, state); got != 7 {
		t.Fatalf("EXPLODE expected first element 7, got %d", got)
	}

	pushInts(t, state, 9, 10, 11)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("failed to push count: %v", err)
	}
	if err := UNTUPLEVAR().Interpret(state); err != nil {
		t.Fatalf("UNTUPLEVAR failed: %v", err)
	}
	if got := popInt(t, state); got != 11 {
		t.Fatalf("UNTUPLEVAR expected last element 11, got %d", got)
	}
	if got := popInt(t, state); got != 10 {
		t.Fatalf("UNTUPLEVAR expected second element 10, got %d", got)
	}
	if got := popInt(t, state); got != 9 {
		t.Fatalf("UNTUPLEVAR expected first element 9, got %d", got)
	}

	pushInts(t, state, 12, 13, 14)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push count: %v", err)
	}
	if err := UNPACKFIRSTVAR().Interpret(state); err != nil {
		t.Fatalf("UNPACKFIRSTVAR failed: %v", err)
	}
	if got := popInt(t, state); got != 13 {
		t.Fatalf("UNPACKFIRSTVAR expected second element 13, got %d", got)
	}
	if got := popInt(t, state); got != 12 {
		t.Fatalf("UNPACKFIRSTVAR expected first element 12, got %d", got)
	}

	pushInts(t, state, 15, 16)
	if err := TUPLE(2).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("failed to push max: %v", err)
	}
	if err := EXPLODEVAR().Interpret(state); err != nil {
		t.Fatalf("EXPLODEVAR failed: %v", err)
	}
	if got := popInt(t, state); got != 2 {
		t.Fatalf("EXPLODEVAR length expected 2, got %d", got)
	}
	if got := popInt(t, state); got != 16 {
		t.Fatalf("EXPLODEVAR expected second element 16, got %d", got)
	}
	if got := popInt(t, state); got != 15 {
		t.Fatalf("EXPLODEVAR expected first element 15, got %d", got)
	}
}

func TestTupleLengthAndLast(t *testing.T) {
	state := newState()

	pushInts(t, state, 1, 2, 3)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := TLEN().Interpret(state); err != nil {
		t.Fatalf("TLEN failed: %v", err)
	}
	if got := popInt(t, state); got != 3 {
		t.Fatalf("TLEN returned %d, want 3", got)
	}

	pushInts(t, state, 4, 5)
	if err := TUPLE(2).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := QTLEN().Interpret(state); err != nil {
		t.Fatalf("QTLEN failed: %v", err)
	}
	if got := popInt(t, state); got != 2 {
		t.Fatalf("QTLEN tuple expected 2, got %d", got)
	}

	if err := state.Stack.PushInt(big.NewInt(10)); err != nil {
		t.Fatalf("failed to push int: %v", err)
	}
	if err := QTLEN().Interpret(state); err != nil {
		t.Fatalf("QTLEN failed on non-tuple: %v", err)
	}
	if got := popInt(t, state); got != -1 {
		t.Fatalf("QTLEN non-tuple expected -1, got %d", got)
	}

	pushInts(t, state, 6, 7, 8)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := ISTUPLE().Interpret(state); err != nil {
		t.Fatalf("ISTUPLE failed: %v", err)
	}
	boolVal, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("failed to pop bool: %v", err)
	}
	if !boolVal {
		t.Fatalf("ISTUPLE expected true for tuple")
	}

	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("failed to push int: %v", err)
	}
	if err := ISTUPLE().Interpret(state); err != nil {
		t.Fatalf("ISTUPLE failed on non-tuple: %v", err)
	}
	boolVal, err = state.Stack.PopBool()
	if err != nil {
		t.Fatalf("failed to pop bool: %v", err)
	}
	if boolVal {
		t.Fatalf("ISTUPLE expected false for non-tuple")
	}

	pushInts(t, state, 9, 10, 11)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := LAST().Interpret(state); err != nil {
		t.Fatalf("LAST failed: %v", err)
	}
	if got := popInt(t, state); got != 11 {
		t.Fatalf("LAST returned %d, want 11", got)
	}
}

func TestTuplePushAndPop(t *testing.T) {
	state := newState()

	pushInts(t, state, 1, 2)
	if err := TUPLE(2).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("failed to push value: %v", err)
	}
	if err := TPUSH().Interpret(state); err != nil {
		t.Fatalf("TPUSH failed: %v", err)
	}
	updated := popAny(t, state)
	tup, ok := updated.(tuplepkg.Tuple)
	if !ok {
		t.Fatalf("expected tuple after TPUSH, got %T", updated)
	}
	if tup.Len() != 3 {
		t.Fatalf("TPUSH expected tuple length 3, got %d", tup.Len())
	}
	item, err := tup.Index(2)
	if err != nil {
		t.Fatalf("tuple index failed: %v", err)
	}
	if got := item.(*big.Int).Int64(); got != 3 {
		t.Fatalf("TPUSH appended %d, want 3", got)
	}

	pushInts(t, state, 4, 5, 6)
	if err := TUPLE(3).Interpret(state); err != nil {
		t.Fatalf("TUPLE failed: %v", err)
	}
	if err := TPOP().Interpret(state); err != nil {
		t.Fatalf("TPOP failed: %v", err)
	}
	if got := popInt(t, state); got != 6 {
		t.Fatalf("TPOP value expected 6, got %d", got)
	}
	updated = popAny(t, state)
	tup, ok = updated.(tuplepkg.Tuple)
	if !ok {
		t.Fatalf("expected tuple after TPOP, got %T", updated)
	}
	if tup.Len() != 2 {
		t.Fatalf("TPOP expected tuple length 2, got %d", tup.Len())
	}
}

func TestTupleNullOperations(t *testing.T) {
	tests := []struct {
		name     string
		op       func() *helpers.SimpleOP
		cond     int64
		initial  []int64
		expected []any
	}{
		{"NULLSWAPIF", func() *helpers.SimpleOP { return NULLSWAPIF() }, 1, []int64{7}, []any{int64(1), nil, int64(7)}},
		{"NULLSWAPIFNOT", func() *helpers.SimpleOP { return NULLSWAPIFNOT() }, 0, []int64{7}, []any{int64(0), nil, int64(7)}},
		{"NULLROTRIF", func() *helpers.SimpleOP { return NULLROTRIF() }, 1, []int64{3, 4}, []any{int64(1), int64(4), nil, int64(3)}},
		{"NULLROTRIFNOT", func() *helpers.SimpleOP { return NULLROTRIFNOT() }, 0, []int64{3, 4}, []any{int64(0), int64(4), nil, int64(3)}},
		{"NULLSWAPIF2", func() *helpers.SimpleOP { return NULLSWAPIF2() }, 1, []int64{5}, []any{int64(1), nil, nil, int64(5)}},
		{"NULLSWAPIFNOT2", func() *helpers.SimpleOP { return NULLSWAPIFNOT2() }, 0, []int64{5}, []any{int64(0), nil, nil, int64(5)}},
		{"NULLROTRIF2", func() *helpers.SimpleOP { return NULLROTRIF2() }, 1, []int64{8, 9}, []any{int64(1), int64(9), nil, nil, int64(8)}},
		{"NULLROTRIFNOT2", func() *helpers.SimpleOP { return NULLROTRIFNOT2() }, 0, []int64{8, 9}, []any{int64(0), int64(9), nil, nil, int64(8)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newState()
			for _, v := range tt.initial {
				if err := state.Stack.PushInt(big.NewInt(v)); err != nil {
					t.Fatalf("failed to push initial value: %v", err)
				}
			}
			if err := state.Stack.PushInt(big.NewInt(tt.cond)); err != nil {
				t.Fatalf("failed to push condition: %v", err)
			}
			if err := tt.op().Interpret(state); err != nil {
				t.Fatalf("operation failed: %v", err)
			}
			for _, exp := range tt.expected {
				val := popAny(t, state)
				switch expected := exp.(type) {
				case nil:
					if val != nil {
						t.Fatalf("expected nil, got %v", val)
					}
				case int64:
					intVal, ok := val.(*big.Int)
					if !ok {
						t.Fatalf("expected int, got %T", val)
					}
					if intVal.Int64() != expected {
						t.Fatalf("expected %d, got %d", expected, intVal.Int64())
					}
				default:
					t.Fatalf("unexpected expected type %T", exp)
				}
			}
			if state.Stack.Len() != 0 {
				t.Fatalf("expected empty stack, got len %d", state.Stack.Len())
			}
		})
	}
}

func TestTupleIndex2AndIndex3(t *testing.T) {
	state := newState()

	inner1 := tuplepkg.NewTuple(big.NewInt(1), big.NewInt(2))
	inner2 := tuplepkg.NewTuple(big.NewInt(3), big.NewInt(4))
	outer := tuplepkg.NewTuple(inner1.Copy(), inner2.Copy())

	if err := state.Stack.PushTuple(outer.Copy()); err != nil {
		t.Fatalf("failed to push outer tuple: %v", err)
	}
	if err := INDEX2(1, 0).Interpret(state); err != nil {
		t.Fatalf("INDEX2 failed: %v", err)
	}
	if got := popInt(t, state); got != 3 {
		t.Fatalf("INDEX2 returned %d, want 3", got)
	}

	deep := tuplepkg.NewTuple(big.NewInt(100), big.NewInt(200))
	mid := tuplepkg.NewTuple(big.NewInt(50), deep.Copy())
	outer3 := tuplepkg.NewTuple(mid.Copy(), big.NewInt(1))

	if err := state.Stack.PushTuple(outer3.Copy()); err != nil {
		t.Fatalf("failed to push nested tuple: %v", err)
	}
	if err := INDEX3(0, 1, 0).Interpret(state); err != nil {
		t.Fatalf("INDEX3 failed: %v", err)
	}
	if got := popInt(t, state); got != 100 {
		t.Fatalf("INDEX3 returned %d, want 100", got)
	}
}
