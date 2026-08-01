package tlb

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestStack_ToCell(t *testing.T) {
	ref := cell.BeginCell().MustStoreInt(-777, 12).EndCell()

	s := NewStack()
	s.Push([]any{})
	s.Push([]any{int64(222)})
	s.Push([]any{int64(555), StackNaN{}})
	s.Push(StackNaN{})
	s.Push(cell.BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(ref))
	s.Push(cell.BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(ref).EndCell())
	s.Push(uint64(18446744073709551615))
	s.Push(int64(555))
	s.Push(cell.BeginCell().MustStoreUInt(0x55, 8).MustStoreRef(ref).EndCell().MustBeginParse())

	c, err := s.ToCell()
	if err != nil {
		t.Fatal("failed to cell", err)
	}

	var s2 Stack
	err = s2.LoadFromCell(c.MustBeginParse())
	if err != nil {
		t.Fatal("failed from cell", err)
	}

	c2, err := s2.ToCell()
	if err != nil {
		t.Fatal("failed to cell", err)
	}

	if !bytes.Equal(c2.Hash(), c.Hash()) {
		t.Fatal("rebuild not same", err)
	}

	v, err := s2.Pop()
	if err != nil {
		t.Fatal("pop 1 err", err)
	}
	slc := v.(*cell.Slice)
	if slc.MustLoadUInt(8) != 0x55 {
		t.Fatal("slice val err", err)
	}
	if !bytes.Equal(slc.MustLoadRef().MustToCell().Hash(), ref.Hash()) {
		t.Fatal("slice ref err", err)
	}

	v, err = s2.Pop()
	if err != nil {
		t.Fatal("pop 1 err", err)
	}
	if v.(*big.Int).Uint64() != 555 {
		t.Fatal("tiny val err", err)
	}

	v, err = s2.Pop()
	if err != nil {
		t.Fatal("pop 1 err", err)
	}
	if v.(*big.Int).Uint64() != 18446744073709551615 {
		t.Fatal("big val err", err)
	}

	s2.Pop()
	s2.Pop()
	s2.Pop()

	v, err = s2.Pop()
	if err != nil {
		t.Fatal("pop 0 err", err)
	}
	if v.([]any)[0].(*big.Int).Uint64() != 555 {
		t.Fatal("tuple big val err", err)
	}

	v, err = s2.Pop()
	if err != nil {
		t.Fatal("pop 0 err", err)
	}
	if v.([]any)[0].(*big.Int).Uint64() != 222 {
		t.Fatal("tuple big val err", err)
	}

	v, err = s2.Pop()
	if err != nil {
		t.Fatal("pop 0 err", err)
	}
	if len(v.([]any)) != 0 {
		t.Fatal("tuple val err", err)
	}
}

func TestParseStackValuePreservesEmptyTuple(t *testing.T) {
	value, err := ParseStackValue(cell.BeginCell().
		MustStoreUInt(0x07, 8).
		MustStoreUInt(0, 16).
		EndCell().MustBeginParse())
	if err != nil {
		t.Fatalf("parse empty tuple: %v", err)
	}

	empty, ok := value.([]any)
	if !ok {
		t.Fatalf("empty tuple type = %T, want []any", value)
	}
	if empty == nil {
		t.Fatal("empty tuple decoded as a nil slice")
	}
	if len(empty) != 0 {
		t.Fatalf("empty tuple length = %d, want 0", len(empty))
	}

	null, err := ParseStackValue(cell.BeginCell().MustStoreUInt(0x00, 8).EndCell().MustBeginParse())
	if err != nil {
		t.Fatalf("parse null: %v", err)
	}
	if null != nil {
		t.Fatalf("null value = %#v, want nil", null)
	}
}

func TestStack_Depth(t *testing.T) {
	stack := NewStack()
	stack.Push(struct{}{})
	stack.Push(struct{}{})
	stack.Push(struct{}{})
	stack.Push(struct{}{})
	dep := stack.Depth()
	if dep != 4 {
		t.Errorf("bad stack depth")
	}
}

func TestParseStackValue(t *testing.T) {
	boc, _ := hex.DecodeString("b5ee9c724101140100be00010607000101020607000a021302000304020005060109040010b020070200080902060700020a13004380115c9efc1f3c4944ec3bc823fa372c3bff194d21a5bf9a0b1a35ea8acc5e8b7e5002000b0c00440200a6e7d0f36eaebcc9b69f116cd65416c8703d9ea2e1e663692fdd3f9c166d41740012010000000005c8f38202000d0c001201000028bd7718211002000e0f020010110012010000000065d8d26802001211001201000000000000000000120100000000076ef1ea00020039c2bf52")
	c, _ := cell.FromBOC(boc)

	vl, err := ParseStackValue(c.MustBeginParse())
	if err != nil {
		t.Fatal(err.Error())
	}

	b := cell.BeginCell()
	err = SerializeStackValue(b, vl)
	if err != nil {
		t.Fatal(err.Error())
	}

	if !bytes.Equal(b.EndCell().Hash(), c.Hash()) {
		t.Fatal("rebuild not same", err)
	}
}

func TestParseStackValueBigInt257RoundTrip(t *testing.T) {
	values := []*big.Int{
		new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63)),
		new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 200)),
		new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256)),
		new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)),
	}

	for _, value := range values {
		t.Run(value.String(), func(t *testing.T) {
			checkStackBigIntRoundTrip(t, value)
		})
	}
}

func TestSerializeStackIntUsesCanonicalBoundaryTag(t *testing.T) {
	minInt64 := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63))
	maxInt64 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 63), big.NewInt(1))
	tests := []struct {
		name    string
		value   *big.Int
		tinyInt bool
	}{
		{name: "min int64", value: minInt64, tinyInt: true},
		{name: "max int64", value: maxInt64, tinyInt: true},
		{name: "below min int64", value: new(big.Int).Sub(new(big.Int).Set(minInt64), big.NewInt(1))},
		{name: "above max int64", value: new(big.Int).Add(new(big.Int).Set(maxInt64), big.NewInt(1))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			builder := cell.BeginCell()
			if err := SerializeStackValue(builder, test.value); err != nil {
				t.Fatal(err)
			}
			encoded := builder.ToSlice()
			if test.tinyInt {
				if tag := encoded.MustLoadUInt(8); tag != 0x01 {
					t.Fatalf("tag = %#x, want vm_stk_tinyint", tag)
				}
				return
			}
			if tag := encoded.MustLoadUInt(15); tag != 0x0200/2 {
				t.Fatalf("tag = %#x, want vm_stk_int", tag)
			}
		})
	}
}

func TestSerializeUnsignedStackIntUsesCanonicalTag(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		tinyInt bool
	}{
		{name: "uint zero", value: uint(0), tinyInt: true},
		{name: "uint64 max int64", value: uint64(math.MaxInt64), tinyInt: true},
		{name: "uint64 above max int64", value: uint64(math.MaxInt64) + 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			builder := cell.BeginCell()
			if err := SerializeStackValue(builder, test.value); err != nil {
				t.Fatal(err)
			}
			encoded := builder.ToSlice()
			if test.tinyInt {
				if tag := encoded.MustLoadUInt(8); tag != 0x01 {
					t.Fatalf("tag = %#x, want vm_stk_tinyint", tag)
				}
				return
			}
			if tag := encoded.MustLoadUInt(15); tag != 0x0200/2 {
				t.Fatalf("tag = %#x, want vm_stk_int", tag)
			}
		})
	}
}

func TestSerializeInvalidBigIntReturnsError(t *testing.T) {
	var nilInt *big.Int
	maxMagnitude := new(big.Int).Lsh(big.NewInt(1), 256)
	tests := []struct {
		name  string
		value *big.Int
	}{
		{name: "typed nil", value: nilInt},
		{name: "positive overflow", value: new(big.Int).Set(maxMagnitude)},
		{name: "negative overflow", value: new(big.Int).Sub(new(big.Int).Neg(maxMagnitude), big.NewInt(1))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := SerializeStackValue(cell.BeginCell(), test.value); err == nil {
				t.Fatal("invalid integer serialized successfully")
			}
		})
	}
}

func TestSerializeTypedNilSliceReturnsError(t *testing.T) {
	var nilSlice *cell.Slice
	if err := SerializeStackValue(cell.BeginCell(), nilSlice); err == nil {
		t.Fatal("typed nil slice serialized successfully")
	}
	if err := SerializeStackValue(cell.BeginCell(), tuple.NewTupleValue(nilSlice)); err == nil {
		t.Fatal("typed nil slice nested in tuple serialized successfully")
	}

	stack := vm.NewStack()
	if err := stack.PushAny(nilSlice); err != nil {
		t.Fatal(err)
	}
	converted, err := NewStackFromVM(stack)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = converted.ToCell(); err == nil {
		t.Fatal("VM stack with typed nil slice serialized successfully")
	}
}

func TestStackLoadRejectsNonEmptyListCells(t *testing.T) {
	nilCell := cell.BeginCell().EndCell()
	nullValue := func(builder *cell.Builder) *cell.Builder {
		return builder.MustStoreUInt(0, 8)
	}

	t.Run("intermediate cons", func(t *testing.T) {
		cons := nullValue(cell.BeginCell().MustStoreRef(nilCell)).MustStoreBoolBit(true).EndCell()
		root := nullValue(cell.BeginCell().MustStoreUInt(2, 24).MustStoreRef(cons)).EndCell()
		var stack Stack
		if err := stack.LoadFromCell(root.MustBeginParse()); err == nil {
			t.Fatal("stack with trailing intermediate data parsed successfully")
		}
	})

	t.Run("terminal nil", func(t *testing.T) {
		terminal := cell.BeginCell().MustStoreBoolBit(true).EndCell()
		root := nullValue(cell.BeginCell().MustStoreUInt(1, 24).MustStoreRef(terminal)).EndCell()
		var stack Stack
		if err := stack.LoadFromCell(root.MustBeginParse()); err == nil {
			t.Fatal("stack with non-empty terminal cell parsed successfully")
		}
	})

	t.Run("outer trailing data remains allowed", func(t *testing.T) {
		root := nullValue(cell.BeginCell().MustStoreUInt(1, 24).MustStoreRef(nilCell)).MustStoreBoolBit(true).EndCell()
		var stack Stack
		if err := stack.LoadFromCell(root.MustBeginParse()); err != nil {
			t.Fatalf("outer trailing data rejected: %v", err)
		}
	})

	t.Run("depth limit", func(t *testing.T) {
		root := cell.BeginCell().MustStoreUInt(maxStackDepth+1, 24).EndCell()
		var stack Stack
		if err := stack.LoadFromCell(root.MustBeginParse()); err == nil {
			t.Fatal("oversized stack parsed successfully")
		}
	})
}

func newIntStack(depth int) *Stack {
	s := NewStack()
	for i := 0; i < depth; i++ {
		s.Push(int64(i))
	}
	return s
}

// C++ Stack::serialize applies no depth check of its own, it is bounded only by the
// uint24 depth field and by the cell depth reached while building the list, so the
// 1024 limit of Stack::deserialize must not leak into the serialization path.
func TestStackSerializationHasNoOwnDepthLimit(t *testing.T) {
	t.Run("at parse limit", func(t *testing.T) {
		encoded, err := newIntStack(maxStackDepth).ToCell()
		if err != nil {
			t.Fatalf("stack of %d values not serialized: %v", maxStackDepth, err)
		}

		values := loadSerializedStackInts(t, encoded)
		if len(values) != maxStackDepth {
			t.Fatalf("serialized values = %d, want %d", len(values), maxStackDepth)
		}
		for i, val := range values {
			if val != int64(i) {
				t.Fatalf("serialized value %d = %d, want %d", i, val, i)
			}
		}

		var parsed Stack
		if err = parsed.LoadFromCell(encoded.MustBeginParse()); err != nil {
			t.Fatalf("stack of %d values not parsed back: %v", maxStackDepth, err)
		}
		if parsed.Depth() != maxStackDepth {
			t.Fatalf("parsed depth = %d, want %d", parsed.Depth(), maxStackDepth)
		}
	})

	// One cons cell per value makes the cell depth limit, not maxStackDepth, the
	// serialization ceiling. C++ hits it as CellBuilder::CellCreateError there.
	t.Run("above cell depth limit", func(t *testing.T) {
		_, err := newIntStack(maxStackDepth + 1).ToCell()
		if err == nil {
			t.Fatal("stack deeper than the cell depth limit serialized successfully")
		}
		if !errors.Is(err, cell.ErrCellDepthLimit) {
			t.Fatalf("serialization error = %v, want %v", err, cell.ErrCellDepthLimit)
		}
	})

	// The declared depth is rejected by the parser alone, before any cell is walked.
	t.Run("declared depth above parse limit", func(t *testing.T) {
		root := cell.BeginCell().MustStoreUInt(1<<24-1, 24).EndCell()
		var parsed Stack
		err := parsed.LoadFromCell(root.MustBeginParse())
		if err == nil {
			t.Fatal("oversized declared depth parsed successfully")
		}
		if !strings.Contains(err.Error(), "stack depth exceeds") {
			t.Fatalf("parse error = %v, want stack depth rejection", err)
		}
	})
}

func TestParseStackTupleRejectsTrailingReferenceCells(t *testing.T) {
	valueWithTrailingData := cell.BeginCell().
		MustStoreUInt(0, 8).
		MustStoreBoolBit(true).
		EndCell()
	encoded := cell.BeginCell().
		MustStoreUInt(0x07, 8).
		MustStoreUInt(1, 16).
		MustStoreRef(valueWithTrailingData).
		EndCell()
	if _, err := ParseStackValue(encoded.MustBeginParse()); err == nil {
		t.Fatal("tuple element with trailing data parsed successfully")
	}

	value := cell.BeginCell().MustStoreUInt(0, 8).EndCell()
	pair := cell.BeginCell().
		MustStoreRef(value).
		MustStoreRef(value).
		MustStoreBoolBit(true).
		EndCell()
	encoded = cell.BeginCell().
		MustStoreUInt(0x07, 8).
		MustStoreUInt(3, 16).
		MustStoreRef(pair).
		MustStoreRef(value).
		EndCell()
	if _, err := ParseStackValue(encoded.MustBeginParse()); err == nil {
		t.Fatal("tuple pair with trailing data parsed successfully")
	}
}

func FuzzParseStackValueBigInt257RoundTrip(f *testing.F) {
	f.Add(uint16(64), false, false)
	f.Add(uint16(64), true, true)
	f.Add(uint16(200), false, false)
	f.Add(uint16(200), true, true)
	f.Add(uint16(256), false, false)
	f.Add(uint16(256), true, false)

	f.Fuzz(func(t *testing.T, rawBit uint16, negative bool, belowPowerOfTwo bool) {
		value := stackBigInt257FuzzValue(rawBit, negative, belowPowerOfTwo)
		checkStackBigIntRoundTrip(t, value)
	})
}

func FuzzStackBigInt257CellRoundTrip(f *testing.F) {
	f.Add(uint16(64), false, false, uint8(0))
	f.Add(uint16(64), true, true, uint8(1))
	f.Add(uint16(200), false, false, uint8(2))
	f.Add(uint16(200), true, true, uint8(3))
	f.Add(uint16(256), false, false, uint8(4))
	f.Add(uint16(256), true, false, uint8(5))

	f.Fuzz(func(t *testing.T, rawBit uint16, negative bool, belowPowerOfTwo bool, rawShape uint8) {
		value := stackBigInt257FuzzValue(rawBit, negative, belowPowerOfTwo)

		stack := NewStack()
		switch rawShape % 6 {
		case 0:
			stack.Push(value)
		case 1:
			stack.Push([]any{value})
		case 2:
			stack.Push([]any{StackNaN{}, value})
		case 3:
			stack.Push([]any{[]any{value}, StackNaN{}})
		case 4:
			stack.Push(StackNaN{})
			stack.Push(value)
		case 5:
			stack.Push(value)
			stack.Push([]any{value, []any{StackNaN{}, value}})
		}

		raw, err := stack.ToCell()
		if err != nil {
			t.Fatalf("serialize stack: %v", err)
		}
		var parsed Stack
		if err = parsed.LoadFromCell(raw.MustBeginParse()); err != nil {
			t.Fatalf("parse stack: %v", err)
		}
		rebuilt, err := parsed.ToCell()
		if err != nil {
			t.Fatalf("serialize parsed stack: %v", err)
		}
		if !bytes.Equal(rebuilt.Hash(), raw.Hash()) {
			t.Fatal("stack should round-trip byte-identically")
		}
	})
}

func FuzzParseStackValueRejectsMalformedBigInt257Prefix(f *testing.F) {
	f.Add(uint8(1), false)
	f.Add(uint8(0x7f), false)
	f.Add(uint8(0x7e), true)

	f.Fuzz(func(t *testing.T, rawPrefix uint8, negative bool) {
		prefix := rawPrefix & 0x7f
		if prefix == 0 {
			prefix = 1
		}

		value := big.NewInt(0)
		if negative && prefix != 0x7f {
			value.Neg(big.NewInt(1))
		}
		raw := cell.BeginCell().
			MustStoreUInt(0x02, 8).
			MustStoreUInt(uint64(prefix), 7).
			MustStoreBigInt(value, 257).
			EndCell()

		if _, err := ParseStackValue(raw.MustBeginParse()); err == nil {
			t.Fatalf("malformed int257 prefix %#x parsed successfully", prefix)
		}
	})
}

func TestParseStackValueRejectsMalformedTupleRefs(t *testing.T) {
	validValue := cell.BeginCell().MustStoreUInt(0x00, 8).EndCell()
	validPairChain := cell.BeginCell().
		MustStoreRef(validValue).
		MustStoreRef(validValue).
		EndCell()

	tests := []struct {
		name string
		raw  *cell.Cell
	}{
		{
			name: "single_missing_value",
			raw: cell.BeginCell().
				MustStoreUInt(0x07, 8).
				MustStoreUInt(1, 16).
				EndCell(),
		},
		{
			name: "single_empty_value",
			raw: cell.BeginCell().
				MustStoreUInt(0x07, 8).
				MustStoreUInt(1, 16).
				MustStoreRef(cell.BeginCell().EndCell()).
				EndCell(),
		},
		{
			name: "pair_missing_tail",
			raw: cell.BeginCell().
				MustStoreUInt(0x07, 8).
				MustStoreUInt(2, 16).
				MustStoreRef(validValue).
				EndCell(),
		},
		{
			name: "triple_missing_chain",
			raw: cell.BeginCell().
				MustStoreUInt(0x07, 8).
				MustStoreUInt(3, 16).
				EndCell(),
		},
		{
			name: "triple_missing_tail",
			raw: cell.BeginCell().
				MustStoreUInt(0x07, 8).
				MustStoreUInt(3, 16).
				MustStoreRef(validPairChain).
				EndCell(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseStackValue(tt.raw.MustBeginParse()); err == nil {
				t.Fatal("malformed tuple parsed successfully")
			}
		})
	}
}

func FuzzParseStackValueRejectsMalformedTupleRefs(f *testing.F) {
	f.Add(uint16(2), uint8(0))
	f.Add(uint16(2), uint8(1))
	f.Add(uint16(3), uint8(2))
	f.Add(uint16(8), uint8(3))

	f.Fuzz(func(t *testing.T, rawLen uint16, rawShape uint8) {
		ln := uint64(rawLen%8) + 2
		validValue := cell.BeginCell().MustStoreUInt(0x00, 8).EndCell()

		raw := cell.BeginCell().
			MustStoreUInt(0x07, 8).
			MustStoreUInt(ln, 16)

		switch rawShape % 4 {
		case 0:
		case 1:
			raw.MustStoreRef(cell.BeginCell().EndCell())
		case 2:
			raw.MustStoreRef(validValue)
		case 3:
			raw.MustStoreRef(cell.BeginCell().MustStoreRef(validValue).EndCell())
		}

		if _, err := ParseStackValue(raw.EndCell().MustBeginParse()); err == nil {
			t.Fatalf("malformed tuple len %d shape %d parsed successfully", ln, rawShape%4)
		}
	})
}

func stackBigInt257FuzzValue(rawBit uint16, negative bool, belowPowerOfTwo bool) *big.Int {
	bit := uint(rawBit%193) + 64
	value := new(big.Int).Lsh(big.NewInt(1), bit)
	if !negative && bit == 256 {
		value.Sub(value, big.NewInt(1))
	} else if belowPowerOfTwo {
		value.Sub(value, big.NewInt(1))
	}
	if negative {
		value.Neg(value)
	}
	return value
}

func checkStackBigIntRoundTrip(t *testing.T, value *big.Int) {
	t.Helper()

	encoded := cell.BeginCell()
	if err := SerializeStackValue(encoded, value); err != nil {
		t.Fatalf("serialize stack value: %v", err)
	}
	raw := encoded.EndCell()

	parsed, err := ParseStackValue(raw.MustBeginParse())
	if err != nil {
		t.Fatalf("parse stack value: %v", err)
	}
	got, ok := parsed.(*big.Int)
	if !ok {
		t.Fatalf("parsed value type = %T, want *big.Int", parsed)
	}
	if got.Cmp(value) != 0 {
		t.Fatalf("parsed value = %s, want %s", got.String(), value.String())
	}

	rebuilt := cell.BeginCell()
	if err = SerializeStackValue(rebuilt, parsed); err != nil {
		t.Fatalf("serialize parsed stack value: %v", err)
	}
	if !bytes.Equal(rebuilt.EndCell().Hash(), raw.Hash()) {
		t.Fatal("stack value should round-trip byte-identically")
	}
}

func TestStackSliceValueUsesBaseCellOffsets(t *testing.T) {
	refA := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	refB := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	base := cell.BeginCell().
		MustStoreUInt(0xABCD, 16).
		MustStoreRef(refA).
		MustStoreRef(refB).
		EndCell()

	value := cell.BeginCell().
		MustStoreUInt(0x04, 8).
		MustStoreRef(base).
		MustStoreUInt(4, 10).
		MustStoreUInt(12, 10).
		MustStoreUInt(1, 3).
		MustStoreUInt(2, 3).
		EndCell()

	parsed, err := ParseStackValue(value.MustBeginParse())
	if err != nil {
		t.Fatalf("parse stack slice: %v", err)
	}
	slc := parsed.(*cell.Slice)
	check := slc.Copy()
	if got := check.MustLoadUInt(8); got != 0xBC {
		t.Fatalf("slice bits = %#x, want 0xbc", got)
	}
	if got := check.MustLoadRef().MustToCell().Hash(); !bytes.Equal(got, refB.Hash()) {
		t.Fatal("slice ref was not restored from the base-cell offset")
	}

	roundTrip := cell.BeginCell()
	if err = SerializeStackValue(roundTrip, parsed); err != nil {
		t.Fatalf("serialize stack slice: %v", err)
	}
	if !bytes.Equal(roundTrip.EndCell().Hash(), value.Hash()) {
		t.Fatal("slice stack value should preserve base cell offsets")
	}
}

func TestStackTupleSliceValuePreservesOffsets(t *testing.T) {
	ref := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	base := cell.BeginCell().MustStoreUInt(0x1234, 16).MustStoreRef(ref).EndCell()
	slc, err := base.MustBeginParse().PreloadSubslice(12, 1)
	if err != nil {
		t.Fatalf("build tuple slice: %v", err)
	}
	if err = slc.SkipBits(4); err != nil {
		t.Fatalf("advance tuple slice: %v", err)
	}

	value := cell.BeginCell()
	if err = SerializeStackValue(value, []any{slc}); err != nil {
		t.Fatalf("serialize tuple: %v", err)
	}
	parsed, err := ParseStackValue(value.EndCell().MustBeginParse())
	if err != nil {
		t.Fatalf("parse tuple: %v", err)
	}
	tup := parsed.([]any)
	if len(tup) != 1 {
		t.Fatalf("tuple len = %d, want 1", len(tup))
	}
	gotSlice := tup[0].(*cell.Slice)
	start, end := gotSlice.BitRange()
	if start != 4 || end != 12 {
		t.Fatalf("tuple slice bit range = %d..%d, want 4..12", start, end)
	}
	if got := gotSlice.MustLoadUInt(8); got != 0x23 {
		t.Fatalf("tuple slice bits = %#x, want 0x23", got)
	}
}

func TestNewStackFromVMSerializesNaN(t *testing.T) {
	vmStack := vm.NewStack()
	overflow := new(big.Int).Lsh(big.NewInt(1), 257)
	if err := vmStack.PushIntQuiet(overflow); err != nil {
		t.Fatalf("push quiet overflow: %v", err)
	}

	stack, err := NewStackFromVM(vmStack)
	if err != nil {
		t.Fatalf("stack from vm: %v", err)
	}

	cellValue, err := stack.ToCell()
	if err != nil {
		t.Fatalf("stack to cell: %v", err)
	}

	var parsed Stack
	if err = parsed.LoadFromCell(cellValue.MustBeginParse()); err != nil {
		t.Fatalf("stack from cell: %v", err)
	}

	val, err := parsed.Pop()
	if err != nil {
		t.Fatalf("pop serialized value: %v", err)
	}
	if _, ok := val.(StackNaN); !ok {
		t.Fatalf("expected StackNaN after serialization, got %T", val)
	}
}

func TestNewStackFromVMPreservesSerializedOrder(t *testing.T) {
	vmStack := vm.NewStack()
	for _, val := range []int64{11, 22, 33} {
		if err := vmStack.PushInt(big.NewInt(val)); err != nil {
			t.Fatalf("push %d: %v", val, err)
		}
	}

	stack, err := NewStackFromVM(vmStack)
	if err != nil {
		t.Fatalf("stack from vm: %v", err)
	}

	cellValue, err := stack.ToCell()
	if err != nil {
		t.Fatalf("stack to cell: %v", err)
	}

	got := loadSerializedStackInts(t, cellValue)
	want := []int64{33, 22, 11}
	if len(got) != len(want) {
		t.Fatalf("serialized stack len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("serialized stack top-to-bottom = %v, want %v", got, want)
		}
	}
}

func loadSerializedStackInts(t *testing.T, stack *cell.Cell) []int64 {
	t.Helper()

	next := stack.MustBeginParse()
	depth, err := next.LoadUInt(24)
	if err != nil {
		t.Fatalf("load stack depth: %v", err)
	}

	res := make([]int64, 0, depth)
	for i := uint64(0); i < depth; i++ {
		rest, err := next.LoadRef()
		if err != nil {
			t.Fatalf("load stack rest %d: %v", i, err)
		}

		val, err := ParseStackValue(next)
		if err != nil {
			t.Fatalf("parse stack value %d: %v", i, err)
		}
		intVal, ok := val.(*big.Int)
		if !ok {
			t.Fatalf("stack value %d type = %T, want *big.Int", i, val)
		}
		if !intVal.IsInt64() {
			t.Fatalf("stack value %d = %s, want int64", i, intVal)
		}
		res = append(res, intVal.Int64())

		next = rest
	}

	if next.BitsLeft() != 0 || next.RefsNum() != 0 {
		t.Fatalf("stack tail is not nil: %d bits, %d refs", next.BitsLeft(), next.RefsNum())
	}

	return res
}
