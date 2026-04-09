package stack

import (
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func newStackState(values ...int64) *vm.State {
	return &vm.State{
		Stack: newStack(values...),
		Gas:   vm.NewGas(),
	}
}

func mustCellHash(t *testing.T, c *cell.Cell) []byte {
	t.Helper()
	if c == nil {
		return nil
	}
	return c.Hash()
}

func mustSliceHash(t *testing.T, s *cell.Slice) []byte {
	t.Helper()
	if s == nil {
		return nil
	}
	return s.MustToCell().Hash()
}

func TestRegisteredStackOpsInstantiate(t *testing.T) {
	if len(vm.List) == 0 {
		t.Fatal("expected stack package to register op getters")
	}

	for i, getter := range vm.List {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("registered getter %d panicked: %v", i, r)
				}
			}()
			if op := getter(); op == nil {
				t.Fatalf("registered getter %d returned nil", i)
			}
		}()
	}
}

func TestPushPopAndXchgRoundTrip(t *testing.T) {
	t.Run("PushRoundTripAndInterpret", func(t *testing.T) {
		src := PUSH(1)
		dst := PUSH(0)
		if err := dst.Deserialize(src.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); got != "s1 PUSH" {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 8 {
			t.Fatalf("unexpected bits: %d", got)
		}

		state := newStackState(1, 2, 3)
		if err := dst.Interpret(state); err != nil {
			t.Fatalf("PUSH failed: %v", err)
		}
		if got := popInts(t, state.Stack, 4); !equalInts(got, []int64{2, 3, 2, 1}) {
			t.Fatalf("unexpected PUSH stack: %v", got)
		}
	})

	t.Run("PopRoundTripAndInterpret", func(t *testing.T) {
		src := POP(1)
		dst := POP(0)
		if err := dst.Deserialize(src.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); got != "s1 POP" {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 8 {
			t.Fatalf("unexpected bits: %d", got)
		}

		state := newStackState(1, 2, 3)
		if err := dst.Interpret(state); err != nil {
			t.Fatalf("POP failed: %v", err)
		}
		if got := popInts(t, state.Stack, 2); !equalInts(got, []int64{3, 1}) {
			t.Fatalf("unexpected POP stack: %v", got)
		}
	})

	t.Run("XchgRoundTripVariants", func(t *testing.T) {
		tests := []struct {
			name string
			src  *OpXCHG
			text string
			bits int64
		}{
			{name: "ZeroShortcut", src: XCHG(0, 3), text: "s0,s3 XCHG", bits: 8},
			{name: "OneShortcut", src: XCHG(1, 4), text: "s1,s4 XCHG", bits: 8},
			{name: "FullEncoding", src: XCHG(2, 5), text: "s2,s5 XCHG", bits: 16},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				dst := XCHG(0, 0)
				if err := dst.Deserialize(tt.src.Serialize().EndCell().BeginParse()); err != nil {
					t.Fatalf("deserialize failed: %v", err)
				}
				if got := dst.SerializeText(); got != tt.text {
					t.Fatalf("unexpected text: %q", got)
				}
				if got := dst.InstructionBits(); got != tt.bits {
					t.Fatalf("unexpected bits: %d", got)
				}
			})
		}

		state := newStackState(1, 2, 3)
		if err := XCHG(0, 2).Interpret(state); err != nil {
			t.Fatalf("XCHG failed: %v", err)
		}
		if got := popInts(t, state.Stack, 3); !equalInts(got, []int64{1, 2, 3}) {
			t.Fatalf("unexpected XCHG stack: %v", got)
		}
	})
}

func TestPushRefAndSliceRoundTrip(t *testing.T) {
	ref := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	sl := cell.BeginCell().MustStoreUInt(0xCD, 8).ToSlice()

	t.Run("PushRefRoundTripAndInterpret", func(t *testing.T) {
		src := PUSHREF(ref)
		dst := PUSHREF(nil)
		if err := dst.Deserialize(src.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); !strings.Contains(got, "PUSHREF") {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 8 {
			t.Fatalf("unexpected bits: %d", got)
		}

		state := newStackState()
		if err := dst.Interpret(state); err != nil {
			t.Fatalf("PUSHREF failed: %v", err)
		}
		got, err := state.Stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop pushed cell: %v", err)
		}
		if string(mustCellHash(t, got)) != string(mustCellHash(t, ref)) {
			t.Fatal("unexpected pushed cell")
		}
	})

	t.Run("PushRefDeserializeRequiresReference", func(t *testing.T) {
		if err := PUSHREF(nil).Deserialize(cell.BeginCell().MustStoreUInt(0x88, 8).EndCell().BeginParse()); err == nil {
			t.Fatal("expected PUSHREF deserialize without ref to fail")
		}
	})

	t.Run("PushSliceRoundTripAndInterpret", func(t *testing.T) {
		src := PUSHSLICE(sl)
		dst := PUSHSLICE(cell.BeginCell().EndCell().BeginParse())
		if err := dst.Deserialize(src.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); !strings.Contains(got, "PUSHREFSLICE") {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 8 {
			t.Fatalf("unexpected bits: %d", got)
		}

		state := newStackState()
		if err := dst.Interpret(state); err != nil {
			t.Fatalf("PUSHSLICE failed: %v", err)
		}
		got, err := state.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop pushed slice: %v", err)
		}
		if string(mustSliceHash(t, got)) != string(mustSliceHash(t, sl)) {
			t.Fatal("unexpected pushed slice")
		}
	})

	t.Run("PushSliceDeserializeRequiresReference", func(t *testing.T) {
		if err := PUSHSLICE(cell.BeginCell().EndCell().BeginParse()).Deserialize(cell.BeginCell().MustStoreUInt(0x89, 8).EndCell().BeginParse()); err == nil {
			t.Fatal("expected PUSHSLICE deserialize without ref to fail")
		}
	})
}

func TestPushSliceInlineForms(t *testing.T) {
	short := cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()
	ref := cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()).ToSlice()
	long := cell.BeginCell().MustStoreSlice(make([]byte, 17), 130).ToSlice()

	tests := []struct {
		name  string
		value *cell.Slice
		form  string
		bits  int64
	}{
		{name: "Short", value: short, form: "SHORT", bits: 12},
		{name: "Ref", value: ref, form: "REF", bits: 15},
		{name: "Long", value: long, form: "LONG", bits: 18},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := PUSHSLICEINLINE(tt.value)
			dst := PUSHSLICEINLINE(cell.BeginCell().EndCell().BeginParse())
			if err := dst.Deserialize(src.Serialize().EndCell().BeginParse()); err != nil {
				t.Fatalf("deserialize failed: %v", err)
			}
			if dst.form != tt.form {
				t.Fatalf("unexpected form: %s", dst.form)
			}
			if got := dst.InstructionBits(); got != tt.bits {
				t.Fatalf("unexpected instruction bits: %d", got)
			}
			if got := dst.SerializeText(); !strings.Contains(got, "PUSHSLICE") {
				t.Fatalf("unexpected text: %q", got)
			}

			state := newStackState()
			if err := dst.Interpret(state); err != nil {
				t.Fatalf("interpret failed: %v", err)
			}
			got, err := state.Stack.PopSlice()
			if err != nil {
				t.Fatalf("failed to pop slice: %v", err)
			}
			if string(mustSliceHash(t, got)) != string(mustSliceHash(t, tt.value)) {
				t.Fatal("unexpected inline slice value")
			}
		})
	}

	t.Run("HelpersHandlePadding", func(t *testing.T) {
		if got := paddedInlineBits(7, 4); got != 12 {
			t.Fatalf("unexpected padded bits: %d", got)
		}
		payload := encodeInlineSlicePayload(cell.BeginCell().MustStoreUInt(1, 1).ToSlice(), 4).EndCell().BeginParse()
		if payload.BitsLeft() != 4 {
			t.Fatalf("unexpected encoded payload bits: %d", payload.BitsLeft())
		}
	})

	t.Run("DeserializeRejectsTooManyRefsInLongForm", func(t *testing.T) {
		code := cell.BeginCell().MustStoreUInt(0x8D, 8).MustStoreUInt(5<<7, 10).EndCell().BeginParse()
		if err := PUSHSLICEINLINE(cell.BeginCell().EndCell().BeginParse()).Deserialize(code); err == nil {
			t.Fatal("expected invalid long inline slice to fail")
		}
	})
}

func TestPushContRoundTripAndInterpret(t *testing.T) {
	small := cell.BeginCell().MustStoreUInt(0xABCD, 16).EndCell()
	big := cell.BeginCell().MustStoreUInt(0xEF, 8).MustStoreRef(cell.BeginCell().MustStoreUInt(0x11, 8).EndCell()).EndCell()
	ref := cell.BeginCell().
		MustStoreUInt(0xAA, 8).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell()

	tests := []struct {
		name string
		cont *cell.Cell
		typ  string
		bits int64
	}{
		{name: "Small", cont: small, typ: "SMALL", bits: 8},
		{name: "Big", cont: big, typ: "BIG", bits: 16},
		{name: "Ref", cont: ref, typ: "REF", bits: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := PUSHCONT(tt.cont)
			serialized := src.Serialize()
			dst := PUSHCONT(nil)
			if err := dst.Deserialize(serialized.EndCell().BeginParse()); err != nil {
				t.Fatalf("deserialize failed: %v", err)
			}
			if dst.typ != tt.typ {
				t.Fatalf("unexpected type: %s", dst.typ)
			}
			if got := dst.InstructionBits(); got != tt.bits {
				t.Fatalf("unexpected instruction bits: %d", got)
			}
			if got := dst.SerializeText(); !strings.Contains(got, tt.typ+" PUSHCONT") {
				t.Fatalf("unexpected text: %q", got)
			}

			state := &vm.State{Stack: vm.NewStack(), Gas: vm.NewGas(), CP: 17}
			if err := dst.Interpret(state); err != nil {
				t.Fatalf("interpret failed: %v", err)
			}

			cont, err := state.Stack.PopContinuation()
			if err != nil {
				t.Fatalf("failed to pop continuation: %v", err)
			}
			ordinary, ok := cont.(*vm.OrdinaryContinuation)
			if !ok {
				t.Fatalf("unexpected continuation type: %T", cont)
			}
			if ordinary.Data.NumArgs != vm.ControlDataAllArgs || ordinary.Data.CP != 17 {
				t.Fatalf("unexpected control data: %+v", ordinary.Data)
			}
			if string(mustCellHash(t, ordinary.Code.MustToCell())) != string(mustCellHash(t, tt.cont)) {
				t.Fatal("unexpected continuation code")
			}
		})
	}
}

func TestPushCtrDictAndLongOps(t *testing.T) {
	t.Run("PushCtrRoundTripAndInterpret", func(t *testing.T) {
		src := PUSHCTR(4)
		dst := PUSHCTR(0)
		if err := dst.Deserialize(src.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); got != "c4 PUSH" {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 16 {
			t.Fatalf("unexpected bits: %d", got)
		}

		ref := cell.BeginCell().MustStoreUInt(0x44, 8).EndCell()
		state := &vm.State{Stack: vm.NewStack(), Gas: vm.NewGas(), Reg: vm.Register{D: [2]*cell.Cell{ref, nil}}}
		if err := dst.Interpret(state); err != nil {
			t.Fatalf("PUSHCTR failed: %v", err)
		}
		got, err := state.Stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop register value: %v", err)
		}
		if string(mustCellHash(t, got)) != string(mustCellHash(t, ref)) {
			t.Fatal("unexpected register value")
		}
	})

	t.Run("DictPushConstRoundTripAndInterpret", func(t *testing.T) {
		ref := cell.BeginCell().MustStoreUInt(0x33, 8).EndCell()
		src := DICTPUSHCONST(ref)
		src.pfx = 511
		dst := DICTPUSHCONST(nil)
		if err := dst.Deserialize(src.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); !strings.Contains(got, "511 DICTPUSHCONST") {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 24 {
			t.Fatalf("unexpected bits: %d", got)
		}

		state := newStackState()
		if err := dst.Interpret(state); err != nil {
			t.Fatalf("DICTPUSHCONST failed: %v", err)
		}
		pfx, err := state.Stack.PopInt()
		if err != nil {
			t.Fatalf("failed to pop prefix: %v", err)
		}
		gotRef, err := state.Stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop ref: %v", err)
		}
		if pfx.Int64() != 511 {
			t.Fatalf("unexpected prefix: %s", pfx.String())
		}
		if string(mustCellHash(t, gotRef)) != string(mustCellHash(t, ref)) {
			t.Fatal("unexpected dict push ref")
		}
	})

	t.Run("LongPushPopRoundTrip", func(t *testing.T) {
		pushDst := PUSHL(0)
		pushCode := PUSHL(2).Serialize().EndCell().BeginParse()
		if _, err := pushCode.LoadUInt(8); err != nil {
			t.Fatalf("failed to skip PUSHL prefix: %v", err)
		}
		if err := pushDst.Deserialize(pushCode); err != nil {
			t.Fatalf("push deserialize failed: %v", err)
		}
		popDst := POPL(0)
		popCode := POPL(1).Serialize().EndCell().BeginParse()
		if _, err := popCode.LoadUInt(8); err != nil {
			t.Fatalf("failed to skip POPL prefix: %v", err)
		}
		if err := popDst.Deserialize(popCode); err != nil {
			t.Fatalf("pop deserialize failed: %v", err)
		}
		if pushDst.SerializeText() != "s2 PUSH" || popDst.SerializeText() != "s1 POP" {
			t.Fatal("unexpected long op text")
		}
		if pushDst.InstructionBits() != 16 || popDst.InstructionBits() != 16 {
			t.Fatal("unexpected long op bits")
		}

		state := newStackState(1, 2, 3)
		if err := pushDst.Interpret(state); err != nil {
			t.Fatalf("PUSHL failed: %v", err)
		}
		if got := popInts(t, state.Stack, 4); !equalInts(got, []int64{1, 3, 2, 1}) {
			t.Fatalf("unexpected PUSHL stack: %v", got)
		}

		state = newStackState(1, 2, 3)
		if err := popDst.Interpret(state); err != nil {
			t.Fatalf("POPL failed: %v", err)
		}
		if got := popInts(t, state.Stack, 2); !equalInts(got, []int64{3, 1}) {
			t.Fatalf("unexpected POPL stack: %v", got)
		}
	})
}

func TestAdditionalStackGuardBranches(t *testing.T) {
	t.Run("CondselPropagatesStackErrors", func(t *testing.T) {
		if err := CONDSEL().Interpret(newStackState(1, 2)); err == nil {
			t.Fatal("expected CONDSEL to fail on a short stack")
		}
	})

	t.Run("DynamicGuardOpsUnderflow", func(t *testing.T) {
		state := newStackState(1)
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push PICK index: %v", err)
		}
		if err := PICK().Interpret(state); err == nil {
			t.Fatal("expected PICK underflow")
		}

		state = newStackState(1, 2)
		if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("failed to push CHKDEPTH depth: %v", err)
		}
		if err := CHKDEPTH().Interpret(state); err == nil {
			t.Fatal("expected CHKDEPTH underflow")
		}

		state = newStackState(1)
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push ONLYTOPX count: %v", err)
		}
		if err := ONLYTOPX().Interpret(state); err == nil {
			t.Fatal("expected ONLYTOPX underflow")
		}

		state = newStackState(1)
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("failed to push ROLL index: %v", err)
		}
		if err := ROLL().Interpret(state); err == nil {
			t.Fatal("expected ROLL underflow")
		}

		state = newStackState(1)
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("failed to push ROLLREV index: %v", err)
		}
		if err := ROLLREV().Interpret(state); err == nil {
			t.Fatal("expected ROLLREV underflow")
		}
	})

	t.Run("HigherArityCopyOps", func(t *testing.T) {
		state := newStackState(1, 2)
		if err := DUP2().Interpret(state); err != nil {
			t.Fatalf("DUP2 failed: %v", err)
		}
		if got := popInts(t, state.Stack, 4); !equalInts(got, []int64{2, 1, 2, 1}) {
			t.Fatalf("unexpected DUP2 stack: %v", got)
		}

		state = newStackState(1, 2, 3, 4)
		if err := OVER2().Interpret(state); err != nil {
			t.Fatalf("OVER2 failed: %v", err)
		}
		if got := popInts(t, state.Stack, 6); !equalInts(got, []int64{2, 1, 4, 3, 2, 1}) {
			t.Fatalf("unexpected OVER2 stack: %v", got)
		}

		state = newStackState(1, 2)
		if err := TUCK().Interpret(state); err != nil {
			t.Fatalf("TUCK failed: %v", err)
		}
		if got := popInts(t, state.Stack, 3); !equalInts(got, []int64{2, 1, 2}) {
			t.Fatalf("unexpected TUCK stack: %v", got)
		}
	})
}

func TestSimpleStackOpsAndHelpers(t *testing.T) {
	t.Run("DropVariantsAndNop", func(t *testing.T) {
		state := newStackState(1, 2, 3)
		if err := DROP().Interpret(state); err != nil {
			t.Fatalf("DROP failed: %v", err)
		}
		if got := popInts(t, state.Stack, 2); !equalInts(got, []int64{2, 1}) {
			t.Fatalf("unexpected DROP stack: %v", got)
		}

		state = newStackState(1, 2, 3)
		if err := DROP2().Interpret(state); err != nil {
			t.Fatalf("DROP2 failed: %v", err)
		}
		if got := popInts(t, state.Stack, 1); !equalInts(got, []int64{1}) {
			t.Fatalf("unexpected DROP2 stack: %v", got)
		}

		state = newStackState(1)
		if err := NOP().Interpret(state); err != nil {
			t.Fatalf("NOP failed: %v", err)
		}
		if got := popInts(t, state.Stack, 1); !equalInts(got, []int64{1}) {
			t.Fatalf("unexpected NOP stack: %v", got)
		}
	})

	t.Run("PopSmallIndexHandlesRangeChecks", func(t *testing.T) {
		state := newStackState()
		if err := state.Stack.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("failed to push index: %v", err)
		}
		idx, err := popSmallIndex(state)
		if err != nil {
			t.Fatalf("popSmallIndex failed: %v", err)
		}
		if idx != 7 {
			t.Fatalf("unexpected index: %d", idx)
		}

		if err := state.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("failed to push negative index: %v", err)
		}
		if _, err = popSmallIndex(state); err == nil {
			t.Fatal("expected negative small index to fail")
		}
	})
}

func TestMultiOpsAndAliases(t *testing.T) {
	simExchange := func(vals []int64, a, b int) {
		vals[a], vals[b] = vals[b], vals[a]
	}
	simPush := func(vals []int64, v int64) []int64 {
		return append([]int64{v}, vals...)
	}

	tests := []struct {
		name     string
		op       *helpers.AdvancedOP
		decoded  *helpers.AdvancedOP
		text     string
		bits     int64
		expected func([]int64) []int64
	}{
		{
			name:    "XC2PU",
			op:      XC2PU(2, 0, 1),
			decoded: XC2PU(0, 0, 0),
			text:    "2,0,1 XC2PU",
			bits:    24,
			expected: func(vals []int64) []int64 {
				simExchange(vals, 1, 2)
				simExchange(vals, 0, 0)
				return simPush(vals, vals[1])
			},
		},
		{
			name:    "XCPUXC",
			op:      XCPUXC(2, 1, 3),
			decoded: XCPUXC(0, 0, 0),
			text:    "2,1,3 XCPUXC",
			bits:    24,
			expected: func(vals []int64) []int64 {
				simExchange(vals, 1, 2)
				vals = simPush(vals, vals[1])
				simExchange(vals, 0, 1)
				simExchange(vals, 0, 3)
				return vals
			},
		},
		{
			name:    "XCPU2",
			op:      XCPU2(1, 0, 2),
			decoded: XCPU2(0, 0, 0),
			text:    "1,0,2 XCPU2",
			bits:    24,
			expected: func(vals []int64) []int64 {
				simExchange(vals, 0, 1)
				vals = simPush(vals, vals[0])
				return simPush(vals, vals[3])
			},
		},
		{
			name:    "PUXC2",
			op:      PUXC2(2, 1, 3),
			decoded: PUXC2(0, 0, 0),
			text:    "2,1,3 PUXC2",
			bits:    24,
			expected: func(vals []int64) []int64 {
				vals = simPush(vals, vals[2])
				simExchange(vals, 2, 0)
				simExchange(vals, 1, 1)
				simExchange(vals, 0, 3)
				return vals
			},
		},
		{
			name:    "PUXCPU",
			op:      PUXCPU(2, 1, 0),
			decoded: PUXCPU(0, 0, 0),
			text:    "2,1,0 PUXCPU",
			bits:    24,
			expected: func(vals []int64) []int64 {
				vals = simPush(vals, vals[2])
				simExchange(vals, 0, 1)
				simExchange(vals, 0, 1)
				return simPush(vals, vals[0])
			},
		},
		{
			name:    "PU2XC",
			op:      PU2XC(2, 1, 3),
			decoded: PU2XC(0, 0, 0),
			text:    "2,1,3 PU2XC",
			bits:    24,
			expected: func(vals []int64) []int64 {
				vals = simPush(vals, vals[2])
				simExchange(vals, 1, 0)
				vals = simPush(vals, vals[1])
				simExchange(vals, 1, 0)
				simExchange(vals, 0, 3)
				return vals
			},
		},
		{
			name:    "PUSH3",
			op:      PUSH3(0, 1, 2),
			decoded: PUSH3(0, 0, 0),
			text:    "0,1,2 PUSH3",
			bits:    24,
			expected: func(vals []int64) []int64 {
				vals = simPush(vals, vals[0])
				vals = simPush(vals, vals[2])
				return simPush(vals, vals[4])
			},
		},
		{
			name:    "PUSH2",
			op:      PUSH2(1, 2),
			decoded: PUSH2(0, 0),
			text:    "1,2 PUSH2",
			bits:    16,
			expected: func(vals []int64) []int64 {
				vals = simPush(vals, vals[1])
				return simPush(vals, vals[3])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.decoded.Deserialize(tt.op.Serialize().EndCell().BeginParse()); err != nil {
				t.Fatalf("deserialize failed: %v", err)
			}
			if got := tt.decoded.SerializeText(); got != tt.text {
				t.Fatalf("unexpected text: %q", got)
			}
			if got := tt.decoded.InstructionBits(); got != tt.bits {
				t.Fatalf("unexpected bits: %d", got)
			}

			initial := []int64{5, 4, 3, 2, 1}
			state := newStackState(1, 2, 3, 4, 5)
			if err := tt.decoded.Interpret(state); err != nil {
				t.Fatalf("interpret failed: %v", err)
			}
			want := tt.expected(append([]int64(nil), initial...))
			if got := popInts(t, state.Stack, len(want)); !equalInts(got, want) {
				t.Fatalf("unexpected stack: %v", got)
			}
		})
	}

	t.Run("PushRefSliceAlias", func(t *testing.T) {
		src := cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()
		op := PUSHREFSLICE(src)
		state := newStackState()
		if err := op.Interpret(state); err != nil {
			t.Fatalf("PUSHREFSLICE failed: %v", err)
		}
		got, err := state.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop alias slice: %v", err)
		}
		if string(mustSliceHash(t, got)) != string(mustSliceHash(t, src)) {
			t.Fatal("unexpected PUSHREFSLICE value")
		}
	})
}

func TestAdditionalPermutationAndBlockOps(t *testing.T) {
	simExchange := func(vals []int64, a, b int) {
		vals[a], vals[b] = vals[b], vals[a]
	}
	simPush := func(vals []int64, v int64) []int64 {
		return append([]int64{v}, vals...)
	}
	simReverse := func(vals []int64, j, i int) {
		for i < j {
			vals[i], vals[j] = vals[j], vals[i]
			i++
			j--
		}
	}
	simRotate := func(vals []int64, from, to int) []int64 {
		simReverse(vals, len(vals)-1, from+to)
		simReverse(vals, from+to-1, 0)
		simReverse(vals, len(vals)-1, 0)
		return vals
	}

	tests := []struct {
		name     string
		op       *helpers.AdvancedOP
		decoded  *helpers.AdvancedOP
		text     string
		bits     int64
		expected func([]int64) []int64
	}{
		{
			name:    "XCHG0",
			op:      XCHG0(2),
			decoded: XCHG0(0),
			text:    "2 XCHG0",
			bits:    8,
			expected: func(vals []int64) []int64 {
				simExchange(vals, 0, 2)
				return vals
			},
		},
		{
			name:    "XCHG0L",
			op:      XCHG0L(3),
			decoded: XCHG0L(0),
			text:    "3 XCHG0",
			bits:    16,
			expected: func(vals []int64) []int64 {
				simExchange(vals, 0, 3)
				return vals
			},
		},
		{
			name:    "XCHG2",
			op:      XCHG2(2, 0),
			decoded: XCHG2(0, 0),
			text:    "2,0 XCHG2",
			bits:    16,
			expected: func(vals []int64) []int64 {
				simExchange(vals, 1, 2)
				simExchange(vals, 0, 0)
				return vals
			},
		},
		{
			name:    "XCHG3",
			op:      XCHG3(3, 1, 0),
			decoded: XCHG3(0, 0, 0),
			text:    "3,1,0 XCHG3",
			bits:    16,
			expected: func(vals []int64) []int64 {
				simExchange(vals, 2, 3)
				simExchange(vals, 1, 1)
				simExchange(vals, 0, 0)
				return vals
			},
		},
		{
			name:    "XCPU",
			op:      XCPU(1, 2),
			decoded: XCPU(0, 0),
			text:    "1,2 XCPU",
			bits:    16,
			expected: func(vals []int64) []int64 {
				simExchange(vals, 0, 1)
				return simPush(vals, vals[2])
			},
		},
		{
			name:    "PUXC",
			op:      PUXC(2, 1),
			decoded: PUXC(0, 0),
			text:    "2,1 PUXC",
			bits:    16,
			expected: func(vals []int64) []int64 {
				vals = simPush(vals, vals[2])
				simExchange(vals, 0, 1)
				simExchange(vals, 0, 1)
				return vals
			},
		},
		{
			name:    "REVERSE",
			op:      REVERSE(1, 1),
			decoded: REVERSE(0, 0),
			text:    "1, 1 REVERSE",
			bits:    16,
			expected: func(vals []int64) []int64 {
				simReverse(vals, 3, 1)
				return vals
			},
		},
		{
			name:    "BLKDROP",
			op:      BLKDROP(2),
			decoded: BLKDROP(0),
			text:    "2 BLKDROP",
			bits:    16,
			expected: func(vals []int64) []int64 {
				return vals[2:]
			},
		},
		{
			name:    "BLKDROP2",
			op:      BLKDROP2(2, 1),
			decoded: BLKDROP2(0, 0),
			text:    "2,1 BLKDROP2",
			bits:    16,
			expected: func(vals []int64) []int64 {
				return append(append([]int64{}, vals[:1]...), vals[3:]...)
			},
		},
		{
			name:    "BLKPUSH",
			op:      BLKPUSH(2, 1),
			decoded: BLKPUSH(0, 0),
			text:    "2, 1 BLKPUSH",
			bits:    16,
			expected: func(vals []int64) []int64 {
				vals = simPush(vals, vals[1])
				return simPush(vals, vals[1])
			},
		},
		{
			name:    "BLKSWAP",
			op:      BLKSWAP(2, 1),
			decoded: BLKSWAP(0, 0),
			text:    "2,1 BLKSWAP",
			bits:    16,
			expected: func(vals []int64) []int64 {
				return simRotate(vals, 3, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.decoded.Deserialize(tt.op.Serialize().EndCell().BeginParse()); err != nil {
				t.Fatalf("deserialize failed: %v", err)
			}
			if got := tt.decoded.SerializeText(); got != tt.text {
				t.Fatalf("unexpected text: %q", got)
			}
			if got := tt.decoded.InstructionBits(); got != tt.bits {
				t.Fatalf("unexpected bits: %d", got)
			}

			want := tt.expected([]int64{5, 4, 3, 2, 1})
			state := newStackState(1, 2, 3, 4, 5)
			if err := tt.decoded.Interpret(state); err != nil {
				t.Fatalf("interpret failed: %v", err)
			}
			if got := popInts(t, state.Stack, len(want)); !equalInts(got, want) {
				t.Fatalf("unexpected stack: %v", got)
			}
		})
	}

	t.Run("BLKSWXAndErrorBranch", func(t *testing.T) {
		dataWant := []int64{4, 3, 5, 2, 1}
		state := newStackState(1, 2, 3, 4, 5)
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push x: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("failed to push y: %v", err)
		}
		if err := BLKSWX().Interpret(state); err != nil {
			t.Fatalf("BLKSWX failed: %v", err)
		}
		if got := popInts(t, state.Stack, len(dataWant)); !equalInts(got, dataWant) {
			t.Fatalf("unexpected BLKSWX stack: %v", got)
		}

		state = newStackState(1, 2, 3)
		if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("failed to push x: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("failed to push y: %v", err)
		}
		if err := BLKSWX().Interpret(state); err == nil {
			t.Fatal("expected BLKSWX underflow")
		}
	})
}

func equalInts(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
