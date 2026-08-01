package tlb

import (
	"bytes"
	"encoding/base64"
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestContinuationCPPFixtures(t *testing.T) {
	tests := []struct {
		name  string
		boc   string
		check func(t *testing.T, cont vm.Continuation)
	}{
		{
			name: "ordinary",
			boc:  "te6ccgEBAgEADwABDgYEAAAIBgABAAaSgHs=",
			check: func(t *testing.T, cont vm.Continuation) {
				ordinary, ok := cont.(*vm.OrdinaryContinuation)
				if !ok {
					t.Fatalf("continuation type = %T, want *vm.OrdinaryContinuation", cont)
				}
				if ordinary.Data.CP != 0 {
					t.Fatalf("codepage = %d, want 0", ordinary.Data.CP)
				}
			},
		},
		{
			name: "envelope",
			boc:  "te6ccgEBAgEADgABBwZgABABAAmAAAAACA==",
			check: func(t *testing.T, cont vm.Continuation) {
				envelope, ok := cont.(*vm.ArgExtContinuation)
				if !ok {
					t.Fatalf("continuation type = %T, want *vm.ArgExtContinuation", cont)
				}
				if envelope.Data.NumArgs != 0 {
					t.Fatalf("argument count = %d, want 0", envelope.Data.NumArgs)
				}
				if _, ok = envelope.Ext.(*vm.QuitContinuation); !ok {
					t.Fatalf("next continuation type = %T, want *vm.QuitContinuation", envelope.Ext)
				}
			},
		},
		{
			name: "quit",
			boc:  "te6ccgEBAQEACAAACwaAAAAACA==",
			check: func(t *testing.T, cont vm.Continuation) {
				quit, ok := cont.(*vm.QuitContinuation)
				if !ok || quit.ExitCode != 0 {
					t.Fatalf("continuation = %#v, want quit(0)", cont)
				}
			},
		},
		{
			name: "exception_quit",
			boc:  "te6ccgEBAQEABAAAAwaY",
			check: func(t *testing.T, cont vm.Continuation) {
				if _, ok := cont.(*vm.ExcQuitContinuation); !ok {
					t.Fatalf("continuation type = %T, want *vm.ExcQuitContinuation", cont)
				}
			},
		},
		{
			name: "repeat",
			boc:  "te6ccgEBBQEANQACEwagAAAAAAAAACgBAgEMBAAAEBAABAIMDAAASBIAAwQADdAaAAAAACAAEnOW7UDtQe1Q5A==",
			check: func(t *testing.T, cont vm.Continuation) {
				repeat, ok := cont.(*vm.RepeatContinuation)
				if !ok || repeat.Count != 2 {
					t.Fatalf("continuation = %#v, want repeat(2)", cont)
				}
			},
		},
		{
			name: "until",
			boc:  "te6ccgEBBQEALAACAwbCAQIBDAQAAAgOAAQCDAwAAEAQAAMEAA3QGgAAAAAgABCW7UDtQe1Q5g==",
			check: func(t *testing.T, cont vm.Continuation) {
				if _, ok := cont.(*vm.UntilContinuation); !ok {
					t.Fatalf("continuation type = %T, want *vm.UntilContinuation", cont)
				}
			},
		},
		{
			name: "again",
			boc:  "te6ccgEBAwEAGAABAwbGAQEMBAAACA4AAgAQlu1A7UHtUOo=",
			check: func(t *testing.T, cont vm.Continuation) {
				if _, ok := cont.(*vm.AgainContinuation); !ok {
					t.Fatalf("continuation type = %T, want *vm.AgainContinuation", cont)
				}
			},
		},
		{
			name: "while_condition",
			boc:  "te6ccgEBBgEANwADAwbKAQIDAQwEAAAIDgAFAQwEAABAEAAFAgwMAABIEgAEBQAN0BoAAAAAIAASlu1A7UHtUJDo",
			check: func(t *testing.T, cont vm.Continuation) {
				loop, ok := cont.(*vm.WhileContinuation)
				if !ok || !loop.CheckCond {
					t.Fatalf("continuation = %#v, want while condition", cont)
				}
			},
		},
		{
			name: "while_body",
			boc:  "te6ccgEBBgEAOAADAwbOAQIDAQwEAAAIBAAFAQwEAAAYEgAFAgwMAABQFAAEBQAN0BoAAAAAIAAUkX+W7UDtQe1Q6A==",
			check: func(t *testing.T, cont vm.Continuation) {
				loop, ok := cont.(*vm.WhileContinuation)
				if !ok || loop.CheckCond {
					t.Fatalf("continuation = %#v, want while body", cont)
				}
			},
		},
		{
			name: "push_int",
			boc:  "te6ccgEBBgEALwABCwb/////+AECDAwAACgKAAIDAgHOBAUACpLtQO35AAsBoAAAAAIACwGgAAAABg==",
			check: func(t *testing.T, cont vm.Continuation) {
				push, ok := cont.(*vm.PushIntContinuation)
				if !ok || push.Int != -1 {
					t.Fatalf("continuation = %#v, want push-int(-1)", cont)
				}
			},
		},
		{
			name: "ordinary_with_control_data",
			boc:  "te6ccgEBCwEAdQADKQYgA4AAAICAAAAAAAAACOAAAMBQBAECAwAAAgFmBAUBMIARkoB77BOIAe1kgAuAFm8CgCFvAgHtZwoBA0A4CgIHUHAAKAYHAgYHAAIICQASAQAAAAAAAAAhABIBAAAAAAAAAAsAEgEAAAAAAAAAFgAEyv4=",
			check: func(t *testing.T, cont vm.Continuation) {
				ordinary, ok := cont.(*vm.OrdinaryContinuation)
				if !ok {
					t.Fatalf("continuation type = %T, want *vm.OrdinaryContinuation", cont)
				}
				if ordinary.Data.NumArgs != 3 || ordinary.Data.CP != 0 {
					t.Fatalf("control data = (args %d, cp %d), want (3, 0)", ordinary.Data.NumArgs, ordinary.Data.CP)
				}
				if ordinary.Data.Stack == nil || ordinary.Data.Stack.Len() != 1 {
					t.Fatalf("captured stack = %#v, want one value", ordinary.Data.Stack)
				}
				captured, err := ordinary.Data.Stack.Get(0)
				if err != nil {
					t.Fatalf("get captured value: %v", err)
				}
				capturedInt, ok := captured.(*big.Int)
				if !ok || capturedInt.Int64() != 17 {
					t.Fatalf("captured value = %#v, want int 17", captured)
				}

				if ordinary.Data.Save.D[0] == nil {
					t.Fatal("saved d0 is nil")
				}
				if got := ordinary.Data.Save.D[0].MustBeginParse().MustLoadUInt(16); got != 0xcafe {
					t.Fatalf("saved d0 = %x, want cafe", got)
				}

				c7 := ordinary.Data.Save.C7
				if c7.IsNull() || c7.Len() != 2 {
					t.Fatalf("saved c7 = %#v, want non-null tuple len 2", c7)
				}
				nestedValue, err := c7.RawIndex(0)
				if err != nil {
					t.Fatalf("get nested c7 tuple: %v", err)
				}
				nested, ok := nestedValue.(tuple.Tuple)
				if !ok || nested.Len() != 2 {
					t.Fatalf("nested c7 value = %#v, want tuple len 2", nestedValue)
				}
				for i, want := range []int64{11, 22} {
					value, err := nested.RawIndex(i)
					if err != nil {
						t.Fatalf("get nested c7 value %d: %v", i, err)
					}
					integer, ok := value.(*big.Int)
					if !ok || integer.Int64() != want {
						t.Fatalf("nested c7 value %d = %#v, want int %d", i, value, want)
					}
				}
				lastValue, err := c7.RawIndex(1)
				if err != nil {
					t.Fatalf("get c7 value 1: %v", err)
				}
				last, ok := lastValue.(*big.Int)
				if !ok || last.Int64() != 33 {
					t.Fatalf("c7 value 1 = %#v, want int 33", lastValue)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			boc, err := base64.StdEncoding.DecodeString(test.boc)
			if err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			fixture, err := cell.FromBOC(boc)
			if err != nil {
				t.Fatalf("parse fixture BOC: %v", err)
			}

			loader := fixture.MustBeginParse()
			value, err := ParseStackValue(loader)
			if err != nil {
				t.Fatalf("parse C++ continuation fixture: %v", err)
			}
			if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
				t.Fatalf("fixture has trailing data: %d bits, %d refs", loader.BitsLeft(), loader.RefsNum())
			}
			cont, ok := value.(vm.Continuation)
			if !ok {
				t.Fatalf("value type = %T, want vm.Continuation", value)
			}
			test.check(t, cont)

			rebuilt := cell.BeginCell()
			if err = SerializeStackValue(rebuilt, cont); err != nil {
				t.Fatalf("serialize parsed continuation: %v", err)
			}
			if !bytes.Equal(rebuilt.EndCell().Hash(), fixture.Hash()) {
				t.Fatal("Go serialization differs from C++ fixture")
			}
		})
	}
}

func TestContinuationRichControlDataRoundTrip(t *testing.T) {
	codeRef0 := cell.BeginCell().MustStoreUInt(0xaa, 8).EndCell()
	codeRef1 := cell.BeginCell().MustStoreUInt(0xbb, 8).EndCell()
	codeBase := cell.BeginCell().
		MustStoreUInt(0xabcd, 16).
		MustStoreRef(codeRef0).
		MustStoreRef(codeRef1).
		EndCell()
	code := codeBase.MustBeginParse()
	if err := code.SkipBitsAndRefs(4, 1); err != nil {
		t.Fatalf("advance code slice: %v", err)
	}
	code, err := code.PreloadSubslice(8, 1)
	if err != nil {
		t.Fatalf("trim code slice: %v", err)
	}

	captured := vm.NewStack()
	if err = captured.PushSmallInt(17); err != nil {
		t.Fatalf("push captured int: %v", err)
	}
	if err = captured.PushHostValue(vm.NaN{}); err != nil {
		t.Fatalf("push captured NaN: %v", err)
	}
	nestedTuple := tuple.NewTupleValue(
		big.NewInt(23),
		tuple.NewTupleValue(vm.NaN{}, &vm.QuitContinuation{ExitCode: 4}),
	)
	if err = captured.PushTuple(nestedTuple); err != nil {
		t.Fatalf("push captured tuple: %v", err)
	}
	if err = captured.PushContinuation(&vm.QuitContinuation{ExitCode: 5}); err != nil {
		t.Fatalf("push captured continuation: %v", err)
	}

	dataCell := cell.BeginCell().MustStoreUInt(0xcafe, 16).EndCell()
	cont := &vm.OrdinaryContinuation{
		Data: vm.ControlData{
			Stack:   captured,
			NumArgs: 3,
			CP:      0,
			Save: vm.Register{
				C: [4]vm.Continuation{
					&vm.QuitContinuation{ExitCode: 6},
					nil,
					&vm.PushIntContinuation{
						Int:  7,
						Next: &vm.ExcQuitContinuation{},
					},
				},
				D:  [2]*cell.Cell{dataCell},
				C7: tuple.NewTupleValue(),
			},
		},
		Code: code,
	}

	beforeStart, beforeEnd := code.BitRange()
	beforeStartRef, beforeEndRef := code.RefRange()
	beforeDepth := captured.Len()

	encoded := cell.BeginCell()
	if err = SerializeStackValue(encoded, cont); err != nil {
		t.Fatalf("serialize continuation: %v", err)
	}
	if captured.Len() != beforeDepth {
		t.Fatalf("serialization changed captured stack depth: got %d want %d", captured.Len(), beforeDepth)
	}
	start, end := code.BitRange()
	startRef, endRef := code.RefRange()
	if start != beforeStart || end != beforeEnd || startRef != beforeStartRef || endRef != beforeEndRef {
		t.Fatalf("serialization changed code range: bits %d..%d refs %d..%d", start, end, startRef, endRef)
	}

	raw := encoded.EndCell()
	parsed, err := ParseStackValue(raw.MustBeginParse())
	if err != nil {
		t.Fatalf("parse continuation: %v", err)
	}
	ordinary, ok := parsed.(*vm.OrdinaryContinuation)
	if !ok {
		t.Fatalf("parsed continuation type = %T, want *vm.OrdinaryContinuation", parsed)
	}
	if ordinary.Data.NumArgs != 3 || ordinary.Data.CP != 0 {
		t.Fatalf("control data = (args %d, cp %d), want (3, 0)", ordinary.Data.NumArgs, ordinary.Data.CP)
	}
	if ordinary.Data.Stack == nil || ordinary.Data.Stack.Len() != 4 {
		t.Fatalf("captured stack = %#v, want four values", ordinary.Data.Stack)
	}
	top, err := ordinary.Data.Stack.Get(0)
	if err != nil {
		t.Fatalf("get captured top: %v", err)
	}
	if continuationExitCode(top) != 5 {
		t.Fatalf("captured top = %#v, want quit(5)", top)
	}
	tupleValue, err := ordinary.Data.Stack.Get(1)
	if err != nil {
		t.Fatalf("get captured tuple: %v", err)
	}
	parsedTuple, ok := tupleValue.(tuple.Tuple)
	if !ok || parsedTuple.Len() != 2 {
		t.Fatalf("captured tuple = %#v, want tuple.Tuple len 2", tupleValue)
	}
	nestedValue, err := parsedTuple.RawIndex(1)
	if err != nil {
		t.Fatalf("get nested tuple: %v", err)
	}
	parsedNested, ok := nestedValue.(tuple.Tuple)
	if !ok || parsedNested.Len() != 2 {
		t.Fatalf("nested tuple = %#v, want tuple.Tuple len 2", nestedValue)
	}
	if nan, err := parsedNested.RawIndex(0); err != nil {
		t.Fatalf("get nested NaN: %v", err)
	} else if _, ok = nan.(vm.NaN); !ok {
		t.Fatalf("nested NaN type = %T, want vm.NaN", nan)
	}
	nan, err := ordinary.Data.Stack.Get(2)
	if err != nil {
		t.Fatalf("get captured NaN: %v", err)
	}
	if _, ok = nan.(vm.NaN); !ok {
		t.Fatalf("captured NaN type = %T, want vm.NaN", nan)
	}
	if ordinary.Data.Save.C[0] == nil || ordinary.Data.Save.C[2] == nil || ordinary.Data.Save.D[0] == nil {
		t.Fatal("saved control registers were not restored")
	}
	if ordinary.Data.Save.C7.IsNull() || ordinary.Data.Save.C7.Len() != 0 {
		t.Fatal("saved non-null empty c7 tuple was not preserved")
	}
	gotStart, gotEnd := ordinary.Code.BitRange()
	gotStartRef, gotEndRef := ordinary.Code.RefRange()
	if gotStart != beforeStart || gotEnd != beforeEnd || gotStartRef != beforeStartRef || gotEndRef != beforeEndRef {
		t.Fatalf("parsed code range: bits %d..%d refs %d..%d", gotStart, gotEnd, gotStartRef, gotEndRef)
	}

	rebuilt := cell.BeginCell()
	if err = SerializeStackValue(rebuilt, ordinary); err != nil {
		t.Fatalf("reserialize continuation: %v", err)
	}
	if !bytes.Equal(rebuilt.EndCell().Hash(), raw.Hash()) {
		t.Fatal("rich continuation did not round-trip byte-identically")
	}
}

func TestContinuationRejectsMalformedValues(t *testing.T) {
	minimalControlData := func(b *cell.Builder) {
		b.MustStoreBoolBit(false).
			MustStoreBoolBit(false).
			MustStoreDict(cell.NewDict(4)).
			MustStoreBoolBit(false)
	}

	tests := []struct {
		name  string
		value *cell.Cell
	}{
		{
			name:  "missing constructor",
			value: cell.BeginCell().MustStoreUInt(0x06, 8).EndCell(),
		},
		{
			name:  "unknown constructor",
			value: cell.BeginCell().MustStoreUInt(0x06, 8).MustStoreUInt(0x2a, 6).EndCell(),
		},
		{
			name: "child continuation has trailing bit",
			value: func() *cell.Cell {
				child := cell.BeginCell().MustStoreUInt(9, 4).MustStoreBoolBit(false).EndCell()
				b := cell.BeginCell().MustStoreUInt(0x06, 8).MustStoreUInt(1, 2)
				minimalControlData(b)
				return b.MustStoreRef(child).EndCell()
			}(),
		},
		{
			name: "present minus one codepage",
			value: func() *cell.Cell {
				return cell.BeginCell().
					MustStoreUInt(0x06, 8).
					MustStoreUInt(0, 2).
					MustStoreBoolBit(false).
					MustStoreBoolBit(false).
					MustStoreDict(cell.NewDict(4)).
					MustStoreBoolBit(true).
					MustStoreInt(-1, 16).
					EndCell()
			}(),
		},
		{
			name:  "invalid saved c6",
			value: malformedSavedRegisterContinuation(t, 6, nil),
		},
		{
			name:  "integer saved as c0",
			value: malformedSavedRegisterContinuation(t, 0, int64(1)),
		},
		{
			name:  "continuation saved as d0",
			value: malformedSavedRegisterContinuation(t, 4, &vm.ExcQuitContinuation{}),
		},
		{
			name:  "null saved as c7",
			value: malformedSavedRegisterContinuation(t, 7, nil),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseStackValue(test.value.MustBeginParse()); err == nil {
				t.Fatal("malformed continuation parsed successfully")
			}
		})
	}
}

func TestContinuationSerializerValidation(t *testing.T) {
	tests := []struct {
		name string
		cont vm.Continuation
	}{
		{name: "quit below int32", cont: &vm.QuitContinuation{ExitCode: -1<<31 - 1}},
		{name: "quit above int32", cont: &vm.QuitContinuation{ExitCode: 1 << 31}},
		{name: "push below int32", cont: &vm.PushIntContinuation{Int: -1<<31 - 1, Next: &vm.ExcQuitContinuation{}}},
		{name: "push above int32", cont: &vm.PushIntContinuation{Int: 1 << 31, Next: &vm.ExcQuitContinuation{}}},
		{name: "negative repeat count", cont: &vm.RepeatContinuation{Count: -1, Body: &vm.ExcQuitContinuation{}, After: &vm.ExcQuitContinuation{}}},
		{name: "missing repeat body", cont: &vm.RepeatContinuation{Count: 1, After: &vm.ExcQuitContinuation{}}},
		{name: "missing ordinary code", cont: &vm.OrdinaryContinuation{Data: vm.ControlData{NumArgs: -1, CP: -1}}},
		{name: "argument count below schema", cont: minimalOrdinaryContinuation(vm.ControlData{NumArgs: -2, CP: -1})},
		{name: "argument count above uint13", cont: minimalOrdinaryContinuation(vm.ControlData{NumArgs: 1 << 13, CP: -1})},
		{name: "codepage below int16", cont: minimalOrdinaryContinuation(vm.ControlData{NumArgs: -1, CP: -1<<15 - 1})},
		{name: "codepage above int16", cont: minimalOrdinaryContinuation(vm.ControlData{NumArgs: -1, CP: 1 << 15})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := SerializeStackValue(cell.BeginCell(), test.cont); err == nil {
				t.Fatal("invalid continuation serialized successfully")
			}
		})
	}

	cycle := &vm.AgainContinuation{}
	cycle.Body = cycle
	if err := SerializeStackValue(cell.BeginCell(), cycle); err == nil {
		t.Fatal("cyclic continuation serialized successfully")
	}

	saveCycle := minimalOrdinaryContinuation(vm.ControlData{NumArgs: -1, CP: -1})
	saveCycle.Data.Save.C[0] = saveCycle
	if err := SerializeStackValue(cell.BeginCell(), saveCycle); err == nil {
		t.Fatal("continuation cycle through saved registers serialized successfully")
	}

	shared := &vm.ExcQuitContinuation{}
	if err := SerializeStackValue(cell.BeginCell(), &vm.RepeatContinuation{
		Count: 1,
		Body:  shared,
		After: shared,
	}); err != nil {
		t.Fatalf("shared acyclic continuation rejected: %v", err)
	}
}

func TestContinuationPreservesOptionalEmptyStack(t *testing.T) {
	tests := []struct {
		name    string
		stack   *vm.Stack
		present bool
	}{
		{name: "absent"},
		{name: "present empty", stack: vm.NewStack(), present: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cont := minimalOrdinaryContinuation(vm.ControlData{
				Stack:   test.stack,
				NumArgs: -1,
				CP:      -1,
			})
			encoded := cell.BeginCell()
			if err := SerializeStackValue(encoded, cont); err != nil {
				t.Fatalf("serialize continuation: %v", err)
			}
			parsed, err := ParseStackValue(encoded.EndCell().MustBeginParse())
			if err != nil {
				t.Fatalf("parse continuation: %v", err)
			}
			stack := parsed.(*vm.OrdinaryContinuation).Data.Stack
			if (stack != nil) != test.present {
				t.Fatalf("captured stack presence = %t, want %t", stack != nil, test.present)
			}
			if stack != nil && stack.Len() != 0 {
				t.Fatalf("captured stack depth = %d, want 0", stack.Len())
			}
		})
	}
}

// C++ ControlData::serialize forwards to the unchecked Stack::serialize, while
// ControlData::deserialize goes through Stack::deserialize and its 1024 limit.
func TestContinuationCapturedStackDepthLimitIsParseOnly(t *testing.T) {
	t.Run("captured stack at parse limit", func(t *testing.T) {
		captured := vm.NewStack()
		for i := 0; i < maxStackDepth; i++ {
			if err := captured.PushSmallInt(int64(i)); err != nil {
				t.Fatalf("push captured int %d: %v", i, err)
			}
		}

		cont := minimalOrdinaryContinuation(vm.ControlData{
			Stack:   captured,
			NumArgs: vm.ControlDataAllArgs,
			CP:      vm.CP,
		})
		encoded := cell.BeginCell()
		if err := SerializeStackValue(encoded, cont); err != nil {
			t.Fatalf("serialize continuation capturing %d values: %v", maxStackDepth, err)
		}

		parsed, err := ParseStackValue(encoded.EndCell().MustBeginParse())
		if err != nil {
			t.Fatalf("parse continuation capturing %d values: %v", maxStackDepth, err)
		}
		stack := parsed.(*vm.OrdinaryContinuation).Data.Stack
		if stack == nil || stack.Len() != maxStackDepth {
			t.Fatalf("captured stack = %#v, want %d values", stack, maxStackDepth)
		}
	})

	t.Run("declared captured depth above parse limit", func(t *testing.T) {
		encoded := cell.BeginCell().
			MustStoreUInt(0x06, 8).             // continuation stack value
			MustStoreUInt(0, 2).                // vmc_std
			MustStoreBoolBit(false).            // nargs: nothing
			MustStoreBoolBit(true).             // stack: just
			MustStoreUInt(maxStackDepth+1, 24). // vm_stack#_ depth
			EndCell()

		_, err := ParseStackValue(encoded.MustBeginParse())
		if err == nil {
			t.Fatal("oversized captured stack parsed successfully")
		}
		if !strings.Contains(err.Error(), "stack depth exceeds") {
			t.Fatalf("parse error = %v, want stack depth rejection", err)
		}
	})
}

func TestStackContinuationRoundTrip(t *testing.T) {
	stack := vm.NewStack()
	if err := stack.PushSmallInt(11); err != nil {
		t.Fatalf("push int: %v", err)
	}
	if err := stack.PushTuple(tuple.NewTupleValue(&vm.QuitContinuation{ExitCode: 12})); err != nil {
		t.Fatalf("push tuple: %v", err)
	}
	if err := stack.PushContinuation(&vm.PushIntContinuation{
		Int:  13,
		Next: &vm.ExcQuitContinuation{},
	}); err != nil {
		t.Fatalf("push continuation: %v", err)
	}

	serialized, err := NewStackFromVM(stack)
	if err != nil {
		t.Fatalf("convert stack: %v", err)
	}
	stackCell, err := serialized.ToCell()
	if err != nil {
		t.Fatalf("serialize stack: %v", err)
	}
	var parsed Stack
	if err = parsed.LoadFromCell(stackCell.MustBeginParse()); err != nil {
		t.Fatalf("parse stack: %v", err)
	}
	rebuilt, err := parsed.ToCell()
	if err != nil {
		t.Fatalf("reserialize stack: %v", err)
	}
	if !bytes.Equal(rebuilt.Hash(), stackCell.Hash()) {
		t.Fatal("stack with continuations did not round-trip byte-identically")
	}
}

func TestNewStackFromVMSnapshotsMutableValues(t *testing.T) {
	vmStack := vm.NewStack()
	if err := vmStack.PushBuilder(cell.BeginCell().MustStoreUInt(0xaa, 8)); err != nil {
		t.Fatalf("push builder: %v", err)
	}

	serialized, err := NewStackFromVM(vmStack)
	if err != nil {
		t.Fatalf("convert stack: %v", err)
	}
	value, err := vmStack.Get(0)
	if err != nil {
		t.Fatalf("get source builder: %v", err)
	}
	if err = value.(*cell.Builder).StoreUInt(0xbb, 8); err != nil {
		t.Fatalf("mutate source builder: %v", err)
	}

	stackCell, err := serialized.ToCell()
	if err != nil {
		t.Fatalf("serialize stack snapshot: %v", err)
	}
	var parsed Stack
	if err = parsed.LoadFromCell(stackCell.MustBeginParse()); err != nil {
		t.Fatalf("parse stack snapshot: %v", err)
	}
	parsedValue, err := parsed.Pop()
	if err != nil {
		t.Fatalf("pop stack snapshot: %v", err)
	}
	builder := parsedValue.(*cell.Builder)
	if builder.BitsUsed() != 8 || builder.ToSlice().MustLoadUInt(8) != 0xaa {
		t.Fatal("NewStackFromVM did not preserve its mutable-value snapshot")
	}
}

func malformedSavedRegisterContinuation(t *testing.T, index uint64, value any) *cell.Cell {
	t.Helper()

	encodedValue := cell.BeginCell()
	if err := SerializeStackValue(encodedValue, value); err != nil {
		t.Fatalf("serialize malformed saved register value: %v", err)
	}
	dict := cell.NewDict(4)
	if err := dict.SetBuilder(cell.BeginCell().MustStoreUInt(index, 4).EndCell(), encodedValue); err != nil {
		t.Fatalf("store malformed saved register: %v", err)
	}

	return cell.BeginCell().
		MustStoreUInt(0x06, 8).
		MustStoreUInt(0, 2).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreDict(dict).
		MustStoreBoolBit(false).
		EndCell()
}

func minimalOrdinaryContinuation(data vm.ControlData) *vm.OrdinaryContinuation {
	return &vm.OrdinaryContinuation{
		Data: data,
		Code: cell.BeginCell().ToSlice(),
	}
}

func continuationExitCode(value any) int64 {
	quit, ok := value.(*vm.QuitContinuation)
	if !ok {
		return -1 << 63
	}
	return quit.ExitCode
}
