package vm

import (
	"math/big"
	"reflect"
	"strings"
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

	if err = stack.PushTuple(tuple.Tuple{}); err != nil {
		t.Fatalf("push null tuple for maybe range pop: %v", err)
	}
	if _, err = stack.PopMaybeTupleRange(1); err == nil {
		t.Fatal("typed null tuple passed PopMaybeTupleRange")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeTypeCheck)
	}

	nullTuple := tuple.Tuple{}
	if err = stack.PushMaybeTuple(&nullTuple); err != nil {
		t.Fatalf("push maybe null tuple: %v", err)
	}
	if got, err := stack.PopAny(); err != nil || got != nil {
		t.Fatalf("maybe null tuple pushed as (%T, %v), want null", got, err)
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
	if err := stack.PushOwnedValue(nilInt); err != nil {
		t.Fatalf("push typed nil integer: %v", err)
	}
	got, err := stack.PopAny()
	if err != nil || got != nil {
		t.Fatalf("typed nil integer normalized to (%T, %v), want nil", got, err)
	}

	var nilContinuation *QuitContinuation
	if err = stack.PushOwnedValue(nilContinuation); err == nil {
		t.Fatal("typed nil continuation was accepted by generic owned boundary")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeTypeCheck)
	}
}

func TestStackPreservesTypedNullReferencesAcrossPushAndSharePaths(t *testing.T) {
	var nilCell *cell.Cell
	var nilBuilder *cell.Builder
	var nilSlice *cell.Slice

	tests := []struct {
		name   string
		value  any
		pushes []struct {
			name string
			push func(*Stack) error
		}
		pop func(*Stack) error
	}{
		{
			name:  "cell",
			value: nilCell,
			pushes: []struct {
				name string
				push func(*Stack) error
			}{
				{name: "PushAny", push: func(s *Stack) error { return s.PushAny(nilCell) }},
				{name: "PushCell", push: func(s *Stack) error { return s.PushCell(nilCell) }},
				{name: "PushOwnedValue", push: func(s *Stack) error { return s.PushOwnedValue(nilCell) }},
			},
			pop: func(s *Stack) error {
				_, err := s.PopCell()
				return err
			},
		},
		{
			name:  "builder",
			value: nilBuilder,
			pushes: []struct {
				name string
				push func(*Stack) error
			}{
				{name: "PushAny", push: func(s *Stack) error { return s.PushAny(nilBuilder) }},
				{name: "PushBuilder", push: func(s *Stack) error { return s.PushBuilder(nilBuilder) }},
				{name: "PushOwnedBuilder", push: func(s *Stack) error { return s.PushOwnedBuilder(nilBuilder) }},
				{name: "PushOwnedValue", push: func(s *Stack) error { return s.PushOwnedValue(nilBuilder) }},
			},
			pop: func(s *Stack) error {
				_, err := s.PopBuilder()
				return err
			},
		},
		{
			name:  "slice",
			value: nilSlice,
			pushes: []struct {
				name string
				push func(*Stack) error
			}{
				{name: "PushAny", push: func(s *Stack) error { return s.PushAny(nilSlice) }},
				{name: "PushSlice", push: func(s *Stack) error { return s.PushSlice(nilSlice) }},
				{name: "PushOwnedSlice", push: func(s *Stack) error { return s.PushOwnedSlice(nilSlice) }},
				{name: "PushOwnedValue", push: func(s *Stack) error { return s.PushOwnedValue(nilSlice) }},
			},
			pop: func(s *Stack) error {
				_, err := s.PopSlice()
				return err
			},
		},
	}

	trace := cell.NewTrace(cell.TraceHooks{OnCreate: func() {}})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, push := range test.pushes {
				t.Run(push.name, func(t *testing.T) {
					stack := NewStack()
					if err := push.push(stack); err != nil {
						t.Fatalf("push typed null reference: %v", err)
					}
					assertTypedNullStackValue(t, stack, 0, test.value)

					stack.SetTrace(trace)
					assertTypedNullStackValue(t, stack, 0, test.value)
					unbound := stack.WithoutTrace(trace)
					assertTypedNullStackValue(t, unbound, 0, test.value)

					if err := stack.PushAt(0); err != nil {
						t.Fatalf("share typed null reference: %v", err)
					}
					copied := stack.Copy()
					for i := 0; i < 2; i++ {
						assertTypedNullStackValue(t, stack, i, test.value)
						assertTypedNullStackValue(t, copied, i, test.value)
					}
					if got := stack.String(); !strings.Contains(got, "null") {
						t.Fatalf("Stack.String() = %q, want typed null marker", got)
					}

					popStack := NewStack()
					if err := push.push(popStack); err != nil {
						t.Fatalf("push for typed pop: %v", err)
					}
					assertVMErrorCode(t, test.pop(popStack), vmerr.CodeTypeCheck)
					if popStack.Len() != 0 {
						t.Fatalf("typed pop left stack depth %d, want 0", popStack.Len())
					}
				})
			}
		})
	}

	stack := NewStack()
	if err := stack.PushCell(nilCell); err != nil {
		t.Fatalf("push typed null cell: %v", err)
	}
	if _, err := stack.PopMaybeCell(); err == nil {
		t.Fatal("typed null cell passed PopMaybeCell")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeTypeCheck)
	}
}

type parityNilContinuation struct{}

func (*parityNilContinuation) GetControlData() *ControlData { return nil }
func (*parityNilContinuation) Jump(*State) (Continuation, error) {
	return nil, nil
}
func (c *parityNilContinuation) Copy() Continuation {
	_ = *c // A typed nil must be normalized before Copy is called.
	return c
}

func TestStackPreservesNullContinuationAcrossExplicitPushAndSharePaths(t *testing.T) {
	var nilContinuation *QuitContinuation
	var nilCustomContinuation *parityNilContinuation

	for _, test := range []struct {
		name string
		push func(*Stack) error
	}{
		{name: "PushContinuation", push: func(s *Stack) error { return s.PushContinuation(nil) }},
		{name: "PushContinuationTypedNil", push: func(s *Stack) error { return s.PushContinuation(nilContinuation) }},
		{name: "PushContinuationCustomTypedNil", push: func(s *Stack) error { return s.PushContinuation(nilCustomContinuation) }},
		{name: "PushOwnedContinuation", push: func(s *Stack) error { return s.PushOwnedContinuation(nil) }},
		{name: "PushOwnedContinuationTypedNil", push: func(s *Stack) error { return s.PushOwnedContinuation(nilContinuation) }},
		{name: "PushOwnedContinuationCustomTypedNil", push: func(s *Stack) error { return s.PushOwnedContinuation(nilCustomContinuation) }},
		{name: "PushAny", push: func(s *Stack) error { return s.PushAny(nullContinuationValue) }},
		{name: "PushAnyTypedNil", push: func(s *Stack) error { return s.PushAny(nilContinuation) }},
		{name: "PushAnyCustomTypedNil", push: func(s *Stack) error { return s.PushAny(nilCustomContinuation) }},
		{name: "PushOwnedValue", push: func(s *Stack) error { return s.PushOwnedValue(nullContinuationValue) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			stack := NewStack()
			if err := test.push(stack); err != nil {
				t.Fatalf("push null continuation: %v", err)
			}
			assertNullContinuationStackValue(t, stack, 0)

			trace := cell.NewTrace(cell.TraceHooks{OnCreate: func() {}})
			stack.SetTrace(trace)
			assertNullContinuationStackValue(t, stack, 0)
			assertNullContinuationStackValue(t, stack.WithoutTrace(trace), 0)
			if err := stack.PushAt(0); err != nil {
				t.Fatalf("share null continuation: %v", err)
			}
			copied := stack.Copy()
			for i := 0; i < 2; i++ {
				assertNullContinuationStackValue(t, stack, i)
				assertNullContinuationStackValue(t, copied, i)
			}
			if got := stack.String(); !strings.Contains(got, "null") {
				t.Fatalf("Stack.String() = %q, want null continuation marker", got)
			}

			popStack := NewStack()
			if err := test.push(popStack); err != nil {
				t.Fatalf("push for typed pop: %v", err)
			}
			_, err := popStack.PopContinuation()
			assertVMErrorCode(t, err, vmerr.CodeTypeCheck)
		})
	}

	tupleValue := tuple.NewTupleValue(nullContinuationValue)
	indexed, err := tupleValue.Index(0)
	if err != nil {
		t.Fatalf("index tuple with null continuation: %v", err)
	}
	if indexed != nullContinuationValue {
		t.Fatalf("tuple continuation = %T %v, want null continuation", indexed, indexed)
	}
}

func TestStackMaybeReferencePushCanonicalizesNull(t *testing.T) {
	var nilCell *cell.Cell
	var nilSlice *cell.Slice

	for _, test := range []struct {
		name string
		push func(*Stack) error
	}{
		{name: "cell", push: func(s *Stack) error { return s.PushMaybeCell(nilCell) }},
		{name: "slice", push: func(s *Stack) error {
			if nilSlice == nil {
				return s.PushAny(nil)
			}
			return s.PushSlice(nilSlice)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stack := NewStack()
			if err := test.push(stack); err != nil {
				t.Fatalf("push maybe reference: %v", err)
			}
			if got, err := stack.PopAny(); err != nil || got != nil {
				t.Fatalf("maybe null reference pushed as (%T, %v), want null", got, err)
			}
		})
	}
}

func TestTuplePreservesTypedNullReferenceTags(t *testing.T) {
	var nilCell *cell.Cell
	var nilBuilder *cell.Builder
	var nilSlice *cell.Slice

	for _, value := range []any{nilCell, nilBuilder, nilSlice} {
		t.Run(reflect.TypeOf(value).String(), func(t *testing.T) {
			tupleValue := tuple.NewTupleValue(value)
			indexed, err := tupleValue.Index(0)
			if err != nil {
				t.Fatalf("index tuple: %v", err)
			}
			assertTypedNullValue(t, indexed, value)

			stack := NewStack()
			if err = stack.PushTuple(tupleValue); err != nil {
				t.Fatalf("push tuple: %v", err)
			}
			popped, err := stack.PopTuple()
			if err != nil {
				t.Fatalf("pop tuple: %v", err)
			}
			indexed, err = popped.Index(0)
			if err != nil {
				t.Fatalf("index popped tuple: %v", err)
			}
			assertTypedNullValue(t, indexed, value)
		})
	}
}

func assertTypedNullStackValue(t *testing.T, stack *Stack, at int, want any) {
	t.Helper()

	got, err := stack.Get(at)
	if err != nil {
		t.Fatalf("get typed null value: %v", err)
	}
	assertTypedNullValue(t, got, want)
}

func assertTypedNullValue(t *testing.T, got, want any) {
	t.Helper()

	if got == nil || reflect.TypeOf(got) != reflect.TypeOf(want) || !reflect.ValueOf(got).IsNil() {
		t.Fatalf("value = %T %#v, want typed null %T", got, got, want)
	}
}

func assertNullContinuationStackValue(t *testing.T, stack *Stack, at int) {
	t.Helper()

	got, err := stack.Get(at)
	if err != nil {
		t.Fatalf("get null continuation: %v", err)
	}
	if got != nullContinuationValue {
		t.Fatalf("value = %T %v, want null continuation", got, got)
	}
}

func TestStackPreservesTypedNullTupleAcrossPushAndSharePaths(t *testing.T) {
	for _, test := range []struct {
		name string
		push func(*Stack) error
	}{
		{name: "PushAny", push: func(stack *Stack) error { return stack.PushAny(tuple.Tuple{}) }},
		{name: "PushTuple", push: func(stack *Stack) error { return stack.PushTuple(tuple.Tuple{}) }},
		{name: "PushOwnedValue", push: func(stack *Stack) error { return stack.PushOwnedValue(tuple.Tuple{}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			stack := NewStack()
			if err := test.push(stack); err != nil {
				t.Fatalf("push typed null tuple: %v", err)
			}
			if err := stack.PushAt(0); err != nil {
				t.Fatalf("share typed null tuple: %v", err)
			}
			copied := stack.Copy()

			for name, source := range map[string]*Stack{"stack": stack, "copy": copied} {
				for source.Len() > 0 {
					value, err := source.PopAny()
					if err != nil {
						t.Fatalf("%s pop: %v", name, err)
					}
					got, ok := value.(tuple.Tuple)
					if !ok || !got.IsNull() {
						t.Fatalf("%s value = %T %#v, want typed null tuple", name, value, value)
					}
				}
			}
		})
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
