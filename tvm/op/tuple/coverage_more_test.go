package tuple

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func mustPushTupleValue(t *testing.T, state *vm.State, vals ...any) {
	t.Helper()
	if err := state.Stack.PushTuple(*tuplepkg.NewTuple(vals...)); err != nil {
		t.Fatalf("failed to push tuple: %v", err)
	}
}

func mustPopTupleValue(t *testing.T, state *vm.State) tuplepkg.Tuple {
	t.Helper()
	tup, err := state.Stack.PopTuple()
	if err != nil {
		t.Fatalf("failed to pop tuple: %v", err)
	}
	return tup
}

func mustDeserializeAdvanced(t *testing.T, op *helpers.AdvancedOP, code *cell.Builder) {
	t.Helper()
	if err := op.Deserialize(code.EndCell().BeginParse()); err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}
}

func TestTupleAdvancedOpSerializationRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		src  *helpers.AdvancedOP
		dst  *helpers.AdvancedOP
		text string
	}{
		{name: "TUPLE", src: TUPLE(5), dst: TUPLE(0), text: "5 TUPLE"},
		{name: "INDEX", src: INDEX(7), dst: INDEX(0), text: "7 INDEX"},
		{name: "INDEXQ", src: INDEXQ(9), dst: INDEXQ(0), text: "9 INDEXQ"},
		{name: "SETINDEX", src: SETINDEX(4), dst: SETINDEX(0), text: "4 SETINDEX"},
		{name: "SETINDEXQ", src: SETINDEXQ(6), dst: SETINDEXQ(0), text: "6 SETINDEXQ"},
		{name: "EXPLODE", src: EXPLODE(3), dst: EXPLODE(0), text: "3 EXPLODE"},
		{name: "UNTUPLE", src: UNTUPLE(2), dst: UNTUPLE(0), text: "2 UNTUPLE"},
		{name: "UNPACKFIRST", src: UNPACKFIRST(1), dst: UNPACKFIRST(0), text: "1 UNPACKFIRST"},
		{name: "INDEX2", src: INDEX2(1, 3), dst: INDEX2(0, 0), text: "INDEX2 1,3"},
		{name: "INDEX3", src: INDEX3(0, 1, 2), dst: INDEX3(0, 0, 0), text: "INDEX3 0,1,2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustDeserializeAdvanced(t, tt.dst, tt.src.Serialize())
			if got := tt.dst.SerializeText(); got != tt.text {
				t.Fatalf("unexpected text: got %q want %q", got, tt.text)
			}
			if got := tt.dst.InstructionBits(); got != 16 {
				t.Fatalf("unexpected instruction bits: %d", got)
			}
		})
	}
}

func TestExecMakeTupleBranches(t *testing.T) {
	t.Run("NegativeCountFailsRangeCheck", func(t *testing.T) {
		if err := execMakeTuple(newState(), -1); err == nil {
			t.Fatal("expected negative tuple size to fail")
		}
	})

	t.Run("StackUnderflowFails", func(t *testing.T) {
		if err := execMakeTuple(newState(), 1); err == nil {
			t.Fatal("expected stack underflow")
		}
	})

	t.Run("TupleVarConsumesCountAndKeepsOrder", func(t *testing.T) {
		state := newState()
		pushInts(t, state, 11, 22)
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push tuple size: %v", err)
		}

		if err := TUPLEVAR().Interpret(state); err != nil {
			t.Fatalf("TUPLEVAR failed: %v", err)
		}

		tup := mustPopTupleValue(t, state)
		for i, want := range []int64{11, 22} {
			val, err := tup.Index(i)
			if err != nil {
				t.Fatalf("failed to read tuple item: %v", err)
			}
			if got := val.(*big.Int).Int64(); got != want {
				t.Fatalf("tuple[%d] = %d, want %d", i, got, want)
			}
		}
	})
}

func TestTupleIndexAndExplodeBranches(t *testing.T) {
	t.Run("Index2NavigatesNestedTuple", func(t *testing.T) {
		state := newState()
		inner := *tuplepkg.NewTuple(big.NewInt(7), big.NewInt(8), big.NewInt(9), big.NewInt(10))
		mustPushTupleValue(t, state, inner, big.NewInt(99))

		if err := INDEX2(0, 2).Interpret(state); err != nil {
			t.Fatalf("INDEX2 failed: %v", err)
		}
		if got := popInt(t, state); got != 9 {
			t.Fatalf("INDEX2 returned %d, want 9", got)
		}
	})

	t.Run("Index3RejectsNonTupleIntermediate", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state, big.NewInt(1), big.NewInt(2))

		if err := INDEX3(0, 0, 0).Interpret(state); err == nil {
			t.Fatal("expected INDEX3 to fail on non-tuple intermediate value")
		}
	})

	t.Run("Index2RejectsOversizedNestedTuple", func(t *testing.T) {
		state := newState()
		oversized := tuplepkg.NewTupleSized(256)
		mustPushTupleValue(t, state, oversized)

		if err := INDEX2(0, 0).Interpret(state); err == nil {
			t.Fatal("expected INDEX2 to reject oversized nested tuple")
		}
	})

	t.Run("ExplodeVarPushesElementsAndLength", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state, big.NewInt(4), big.NewInt(5))
		if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("failed to push max tuple size: %v", err)
		}

		if err := EXPLODEVAR().Interpret(state); err != nil {
			t.Fatalf("EXPLODEVAR failed: %v", err)
		}

		if got := popInt(t, state); got != 2 {
			t.Fatalf("unexpected exploded length: %d", got)
		}
		if got := popInt(t, state); got != 5 {
			t.Fatalf("unexpected second exploded item: %d", got)
		}
		if got := popInt(t, state); got != 4 {
			t.Fatalf("unexpected first exploded item: %d", got)
		}
	})
}

func TestUntupleVariants(t *testing.T) {
	t.Run("UntupleVarExactMatch", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state, big.NewInt(1), big.NewInt(2))
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push count: %v", err)
		}

		if err := UNTUPLEVAR().Interpret(state); err != nil {
			t.Fatalf("UNTUPLEVAR failed: %v", err)
		}

		if got := popInt(t, state); got != 2 {
			t.Fatalf("unexpected second untupled item: %d", got)
		}
		if got := popInt(t, state); got != 1 {
			t.Fatalf("unexpected first untupled item: %d", got)
		}
	})

	t.Run("UnpackFirstVarLimitsToTupleLength", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state, big.NewInt(3), big.NewInt(4))
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push count: %v", err)
		}

		if err := UNPACKFIRSTVAR().Interpret(state); err != nil {
			t.Fatalf("UNPACKFIRSTVAR failed: %v", err)
		}

		if state.Stack.Len() != 2 {
			t.Fatalf("unexpected stack depth after UNPACKFIRSTVAR: %d", state.Stack.Len())
		}
		if got := popInt(t, state); got != 4 {
			t.Fatalf("unexpected second unpacked item: %d", got)
		}
		if got := popInt(t, state); got != 3 {
			t.Fatalf("unexpected first unpacked item: %d", got)
		}
	})

	t.Run("UntupleRejectsWrongExactSize", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state, big.NewInt(1))

		if err := UNTUPLE(2).Interpret(state); err == nil {
			t.Fatal("expected UNTUPLE exact-size mismatch to fail")
		}
	})
}

func TestSetIndexQuietBranches(t *testing.T) {
	t.Run("NilTupleAndNilValueStayNil", func(t *testing.T) {
		state := newState()
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("failed to push nil value: %v", err)
		}
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("failed to push nil tuple: %v", err)
		}

		if err := SETINDEXQ(3).Interpret(state); err != nil {
			t.Fatalf("SETINDEXQ failed: %v", err)
		}
		if got := popAny(t, state); got != nil {
			t.Fatalf("expected nil result, got %v", got)
		}
	})

	t.Run("NilTupleAllocatesAndStoresValue", func(t *testing.T) {
		state := newState()
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("failed to push nil tuple: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(77)); err != nil {
			t.Fatalf("failed to push value: %v", err)
		}

		if err := SETINDEXQ(2).Interpret(state); err != nil {
			t.Fatalf("SETINDEXQ failed: %v", err)
		}

		tup := mustPopTupleValue(t, state)
		if tup.Len() != 3 {
			t.Fatalf("unexpected tuple length: %d", tup.Len())
		}
		val, err := tup.Index(2)
		if err != nil {
			t.Fatalf("failed to read stored value: %v", err)
		}
		if got := val.(*big.Int).Int64(); got != 77 {
			t.Fatalf("unexpected stored value: %d", got)
		}
	})
}

func TestTupleSimpleOps(t *testing.T) {
	t.Run("LengthAndLast", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state, big.NewInt(5), big.NewInt(6), big.NewInt(7))

		if err := LAST().Interpret(state); err != nil {
			t.Fatalf("LAST failed: %v", err)
		}
		if got := popInt(t, state); got != 7 {
			t.Fatalf("unexpected LAST value: %d", got)
		}

		mustPushTupleValue(t, state, big.NewInt(1), big.NewInt(2), big.NewInt(3))
		if err := TLEN().Interpret(state); err != nil {
			t.Fatalf("TLEN failed: %v", err)
		}
		if got := popInt(t, state); got != 3 {
			t.Fatalf("unexpected TLEN value: %d", got)
		}
	})

	t.Run("PushAndPopTupleTail", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state, big.NewInt(1), big.NewInt(2))
		if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("failed to push tuple tail value: %v", err)
		}

		if err := TPUSH().Interpret(state); err != nil {
			t.Fatalf("TPUSH failed: %v", err)
		}
		tup := mustPopTupleValue(t, state)
		if tup.Len() != 3 {
			t.Fatalf("unexpected tuple length after TPUSH: %d", tup.Len())
		}

		if err := state.Stack.PushTuple(tup); err != nil {
			t.Fatalf("failed to re-push tuple: %v", err)
		}
		if err := TPOP().Interpret(state); err != nil {
			t.Fatalf("TPOP failed: %v", err)
		}

		if got := popInt(t, state); got != 3 {
			t.Fatalf("unexpected popped tail value: %d", got)
		}
		tup = mustPopTupleValue(t, state)
		if tup.Len() != 2 {
			t.Fatalf("unexpected tuple length after TPOP: %d", tup.Len())
		}
	})

	t.Run("SetIndexRejectsOutOfRange", func(t *testing.T) {
		state := newState()
		mustPushTupleValue(t, state, big.NewInt(1))
		if err := state.Stack.PushInt(big.NewInt(9)); err != nil {
			t.Fatalf("failed to push replacement value: %v", err)
		}

		if err := SETINDEX(5).Interpret(state); err == nil {
			t.Fatal("expected SETINDEX out-of-range failure")
		}
	})
}
