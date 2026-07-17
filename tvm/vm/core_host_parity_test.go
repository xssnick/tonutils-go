package vm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestStackDistinguishesNullAndEmptyTuple(t *testing.T) {
	empty := tuple.NewTupleSized(0)
	if empty.IsNull() {
		t.Fatal("empty tuple is null")
	}

	stack := NewStack()
	if err := stack.PushTuple(empty); err != nil {
		t.Fatalf("push empty tuple: %v", err)
	}
	got, err := stack.PopTuple()
	if err != nil {
		t.Fatalf("pop empty tuple: %v", err)
	}
	if got.IsNull() || got.Len() != 0 {
		t.Fatalf("popped empty tuple = null:%v len:%d", got.IsNull(), got.Len())
	}

	if err = stack.PushTuple(tuple.Tuple{}); err != nil {
		t.Fatalf("push null tuple: %v", err)
	}
	if _, err = stack.PopTuple(); err == nil {
		t.Fatal("null tuple passed PopTuple")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeTypeCheck)
	}

	if err = stack.PushTuple(tuple.Tuple{}); err != nil {
		t.Fatalf("push null tuple for range pop: %v", err)
	}
	if _, err = stack.PopTupleRange(1); err == nil {
		t.Fatal("null tuple passed PopTupleRange")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeTypeCheck)
	}

	one := tuple.NewTupleValue(big.NewInt(1))
	if err = stack.PushTuple(one); err != nil {
		t.Fatalf("push one-element tuple: %v", err)
	}
	if got, err = stack.PopTupleRange(1, 1); err != nil || got.Len() != 1 {
		t.Fatalf("pop one-element tuple = (%v, %v)", got, err)
	}
}

func TestHostBoundaryRejectsInvalidStackArgumentsAndValues(t *testing.T) {
	reg := Register{}
	if _, ok := reg.Get(-1).(Null); !ok {
		t.Fatalf("negative register index returned %T, want Null", reg.Get(-1))
	}

	stack := NewStack()
	pushInts(t, stack, 1, 2)
	for name, fn := range map[string]func() error{
		"drop":           func() error { return stack.Drop(-1) },
		"drop_after":     func() error { return stack.DropAfter(-1) },
		"drop_many_num":  func() error { return stack.DropMany(-1, 0) },
		"drop_many_offs": func() error { return stack.DropMany(0, -1) },
		"from_top": func() error {
			_, err := stack.FromTop(-1)
			return err
		},
		"from_top_len": func() error {
			_, err := stack.FromTop(stack.Len())
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := fn()
			assertVMErrorCode(t, err, vmerr.CodeStackUnderflow)
			if stack.Len() != 2 {
				t.Fatalf("stack length changed to %d", stack.Len())
			}
		})
	}

	if err := stack.PushOwnedValue(struct{}{}); err == nil {
		t.Fatal("unsupported owned value was accepted")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeTypeCheck)
	}
	if stack.Len() != 2 {
		t.Fatalf("invalid owned push changed stack length to %d", stack.Len())
	}

	tooLarge := new(big.Int).Lsh(big.NewInt(1), 256)
	if err := stack.PushOwnedValue(tooLarge); err == nil {
		t.Fatal("oversized owned integer was accepted")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeIntOverflow)
	}

	var nilInt *big.Int
	var nilCell *cell.Cell
	var nilBuilder *cell.Builder
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "integer", value: nilInt},
		{name: "cell", value: nilCell},
		{name: "builder", value: nilBuilder},
		{name: "tuple", value: tuple.Tuple{}},
	} {
		if err := stack.PushOwnedValue(test.value); err != nil {
			t.Fatalf("push typed nil %s: %v", test.name, err)
		}
		if got, err := stack.PopAny(); err != nil || got != nil {
			t.Fatalf("typed nil %s normalized to (%T, %v), want nil", test.name, got, err)
		}
	}

	var nilSlice *cell.Slice
	if err := stack.PushOwnedValue(nilSlice); err != nil {
		t.Fatalf("push typed nil slice: %v", err)
	}
	got, err := stack.PopAny()
	if err != nil {
		t.Fatalf("pop typed nil slice: %v", err)
	}
	if slice, ok := got.(*cell.Slice); !ok || slice != nil {
		t.Fatalf("typed nil slice = %#v, want typed nil slice", got)
	}

	var nilContinuation *QuitContinuation
	if err := stack.PushOwnedValue(nilContinuation); err == nil {
		t.Fatal("typed nil continuation was accepted")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeTypeCheck)
	}
}

func TestGasRejectsNegativeConsumption(t *testing.T) {
	gas := GasWithLimit(100)
	remaining := gas.Remaining
	if err := gas.Consume(-1); err == nil {
		t.Fatal("negative gas consumption succeeded")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeRangeCheck)
	}
	if gas.Remaining != remaining {
		t.Fatalf("negative gas consumption changed remaining to %d", gas.Remaining)
	}

	for _, test := range []struct {
		name    string
		version int
	}{
		{name: "legacy", version: 3},
		{name: "checked", version: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := NewExecutionState(test.version, GasWithLimit(100), nil, tuple.Tuple{}, NewStack())
			remaining := state.Gas.Remaining
			if err := state.ConsumeGas(-1); err == nil {
				t.Fatal("negative state gas consumption succeeded")
			} else {
				assertVMErrorCode(t, err, vmerr.CodeRangeCheck)
			}
			if state.Gas.Remaining != remaining {
				t.Fatalf("negative state gas changed remaining to %d", state.Gas.Remaining)
			}
		})
	}

	state := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100), nil, tuple.Tuple{}, NewStack())
	remaining = state.Gas.Remaining
	if err := state.ConsumeTupleGasLen(-1); err == nil {
		t.Fatal("negative tuple gas length succeeded")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeRangeCheck)
	}
	if state.Gas.Remaining != remaining {
		t.Fatalf("negative tuple gas changed remaining to %d", state.Gas.Remaining)
	}
}

func TestWideCountersAndChildStepAggregation(t *testing.T) {
	const maxUint32 = uint64(1<<32 - 1)

	parent := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	parent.SignatureCheckCounter = maxUint32
	if err := parent.RegisterSignatureCheckCall(); err != nil {
		t.Fatalf("register signature check: %v", err)
	}
	if parent.SignatureCheckCounter != maxUint32+1 {
		t.Fatalf("signature counter = %d, want %d", parent.SignatureCheckCounter, maxUint32+1)
	}

	parent.GetExtraBalanceCounter = maxUint32
	parent.RegisterGetExtraBalanceCall()
	if parent.GetExtraBalanceCounter != maxUint32+1 {
		t.Fatalf("extra-balance counter = %d, want %d", parent.GetExtraBalanceCounter, maxUint32+1)
	}

	parent.Steps = maxUint32 + 2
	parent.SetChildRunner(func(child *State) (int64, error) {
		child.Steps = maxUint32 + 3
		return 0, nil
	})
	if err := parent.RunChildVM(ChildVMConfig{
		Code: cell.BeginCell().EndCell().MustBeginParse(),
		Gas:  GasWithLimit(10),
	}); err != nil {
		t.Fatalf("run child VM: %v", err)
	}
	wantSteps := 2*maxUint32 + 5
	if parent.Steps != wantSteps {
		t.Fatalf("aggregated steps = %d, want %d", parent.Steps, wantSteps)
	}
}
