package helpers

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestPrefixHelpers(t *testing.T) {
	src := []byte{0xA0}
	slicePrefix := SlicePrefix(3, src)
	src[0] = 0x00
	if slicePrefix.Data[0] != 0xA0 {
		t.Fatalf("SlicePrefix should clone input data")
	}

	bytePrefix := BytesPrefix(0x12, 0x34)
	uintPrefix := UIntPrefix(0x15, 5)
	prefixed := NewPrefixed(bytePrefix, slicePrefix)
	single := SinglePrefixed(uintPrefix)

	if len(prefixed.Prefixes) != 2 {
		t.Fatalf("unexpected prefixed size: %d", len(prefixed.Prefixes))
	}
	if len(single.GetPrefixes()) != 1 {
		t.Fatalf("unexpected single prefix count")
	}

	slices := PrefixSlices(bytePrefix, slicePrefix, uintPrefix)
	if len(slices) != 3 {
		t.Fatalf("unexpected prefix slice count: %d", len(slices))
	}

	if got := slices[0].MustLoadUInt(16); got != 0x1234 {
		t.Fatalf("unexpected byte prefix value: %#x", got)
	}
	if got := slices[1].MustLoadUInt(3); got != 0x5 {
		t.Fatalf("unexpected slice prefix value: %#x", got)
	}
	if got := uintPrefix.ToSlice().MustLoadUInt(5); got != 0x15 {
		t.Fatalf("unexpected uint prefix value: %#x", got)
	}

	builder := Builder([]byte{0xDE, 0xAD})
	if got := builder.EndCell().BeginParse().MustLoadUInt(16); got != 0xDEAD {
		t.Fatalf("unexpected builder value: %#x", got)
	}
}

func TestSimpleOPLifecycle(t *testing.T) {
	called := 0
	op := &SimpleOP{
		Action: func(state *vm.State) error {
			called++
			return state.Stack.PushBool(true)
		},
		BitPrefix:    UIntPrefix(0x1A, 5),
		Name:         "SIMPLE",
		BaseGasPrice: 3,
	}

	if len(op.GetPrefixes()) != 1 {
		t.Fatalf("unexpected prefix count")
	}

	encoded := op.Serialize().EndCell()
	if got := encoded.BeginParse().MustLoadUInt(5); got != 0x1A {
		t.Fatalf("unexpected serialized prefix: %#x", got)
	}

	if op.SerializeText() != "SIMPLE" {
		t.Fatalf("unexpected serialize text: %q", op.SerializeText())
	}
	if op.InstructionBits() != 5 {
		t.Fatalf("unexpected instruction bits: %d", op.InstructionBits())
	}

	if err := op.Deserialize(encoded.BeginParse()); err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}

	shortCode := cell.BeginCell().MustStoreUInt(0x1, 1).EndCell().BeginParse()
	if err := op.DeserializeMatched(shortCode); err == nil {
		t.Fatal("expected deserialize underflow")
	}

	state := &vm.State{Stack: vm.NewStack(), Gas: vm.GasWithLimit(10)}
	if err := op.Interpret(state); err != nil {
		t.Fatalf("interpret failed: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected action to be called once, got %d", called)
	}
	if state.Gas.Remaining != 7 {
		t.Fatalf("unexpected gas remaining: %d", state.Gas.Remaining)
	}

	got, err := state.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop bool: %v", err)
	}
	if !got {
		t.Fatal("expected true result on stack")
	}

	denied := 0
	noGas := &SimpleOP{
		Action: func(state *vm.State) error {
			denied++
			return nil
		},
		BitPrefix:    BytesPrefix(0xFF),
		Name:         "NOPE",
		BaseGasPrice: 2,
	}
	state = &vm.State{Stack: vm.NewStack(), Gas: vm.GasWithLimit(1)}
	if err := noGas.Interpret(state); err == nil {
		t.Fatal("expected out of gas")
	}
	if denied != 0 {
		t.Fatalf("action should not run on gas failure")
	}
}

func TestAdvancedOPLifecycle(t *testing.T) {
	var decoded uint64
	called := 0

	op := &AdvancedOP{
		Action: func(state *vm.State) error {
			called++
			return state.Stack.PushInt(big.NewInt(77))
		},
		BitPrefix: helpersPrefixForTest(),
		NameSerializer: func() string {
			return "ADVANCED"
		},
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(0x5, 3)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(3)
			if err != nil {
				return err
			}
			decoded = val
			return nil
		},
		BaseGasPrice: 4,
		FixedSizeBits: 3,
	}

	if len(op.GetPrefixes()) != 1 {
		t.Fatalf("unexpected prefix count")
	}
	if op.SerializeText() != "ADVANCED" {
		t.Fatalf("unexpected name: %q", op.SerializeText())
	}
	if op.InstructionBits() != 7 {
		t.Fatalf("unexpected instruction bits: %d", op.InstructionBits())
	}

	serialized := op.Serialize().EndCell()
	if got := serialized.BeginParse().MustLoadUInt(7); got != 0b1010101 {
		t.Fatalf("unexpected serialized value: %#b", got)
	}
	if err := op.Deserialize(serialized.BeginParse()); err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}
	if decoded != 0x5 {
		t.Fatalf("unexpected decoded suffix: %d", decoded)
	}

	state := &vm.State{Stack: vm.NewStack(), Gas: vm.GasWithLimit(10)}
	if err := op.Interpret(state); err != nil {
		t.Fatalf("interpret failed: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected action to be called once, got %d", called)
	}
	got, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop int: %v", err)
	}
	if got.Int64() != 77 {
		t.Fatalf("unexpected pushed int: %s", got.String())
	}

	plain := &AdvancedOP{
		Action:         func(*vm.State) error { return nil },
		BitPrefix:      BytesPrefix(0x80),
		NameSerializer: func() string { return "PLAIN" },
	}
	if err := plain.Deserialize(plain.Serialize().EndCell().BeginParse()); err != nil {
		t.Fatalf("plain deserialize failed: %v", err)
	}

	state = &vm.State{Stack: vm.NewStack(), Gas: vm.GasWithLimit(1)}
	failing := &AdvancedOP{
		Action:         func(*vm.State) error { t.Fatal("action should not run"); return nil },
		BitPrefix:      BytesPrefix(0x81),
		NameSerializer: func() string { return "FAIL" },
		BaseGasPrice:   2,
	}
	if err := failing.Interpret(state); err == nil {
		t.Fatal("expected out of gas")
	}
}

func TestMathHelpers(t *testing.T) {
	q, r := DivFloor(big.NewInt(7), big.NewInt(-3))
	if q.Int64() != -3 || r.Int64() != -2 {
		t.Fatalf("unexpected floor division result: q=%s r=%s", q, r)
	}

	q, r = DivFloor(big.NewInt(7), big.NewInt(3))
	if q.Int64() != 2 || r.Int64() != 1 {
		t.Fatalf("unexpected positive floor division result: q=%s r=%s", q, r)
	}

	if got := DivRound(big.NewInt(7), big.NewInt(2)); got.Int64() != 4 {
		t.Fatalf("unexpected rounded result: %s", got)
	}
	if got := DivRound(big.NewInt(-7), big.NewInt(2)); got.Int64() != -3 {
		t.Fatalf("unexpected negative rounded result: %s", got)
	}

	if got := DivCeil(big.NewInt(7), big.NewInt(3)); got.Int64() != 3 {
		t.Fatalf("unexpected ceil result: %s", got)
	}
	if got := DivCeil(big.NewInt(-7), big.NewInt(3)); got.Int64() != -2 {
		t.Fatalf("unexpected negative ceil result: %s", got)
	}
}

func helpersPrefixForTest() BitPrefix {
	return SlicePrefix(4, []byte{0xA0})
}
