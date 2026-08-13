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
	if got := builder.EndCell().MustBeginParse().MustLoadUInt(16); got != 0xDEAD {
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
		MinVersion:   13,
	}

	if len(op.GetPrefixes()) != 1 {
		t.Fatalf("unexpected prefix count")
	}

	encoded := op.Serialize().EndCell()
	if got := encoded.MustBeginParse().MustLoadUInt(5); got != 0x1A {
		t.Fatalf("unexpected serialized prefix: %#x", got)
	}

	if op.SerializeText() != "SIMPLE" {
		t.Fatalf("unexpected serialize text: %q", op.SerializeText())
	}
	if op.InstructionBits() != 5 {
		t.Fatalf("unexpected instruction bits: %d", op.InstructionBits())
	}
	if op.MinGlobalVersion() != 13 {
		t.Fatalf("unexpected min version: %d", op.MinGlobalVersion())
	}
	if err := op.Deserialize(encoded.MustBeginParse()); err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}

	shortCode := cell.BeginCell().MustStoreUInt(0x1, 1).EndCell().MustBeginParse()
	if err := op.DeserializeMatched(shortCode); err == nil {
		t.Fatal("expected deserialize underflow")
	}

	state := &vm.State{Stack: vm.NewStack(), Gas: vm.GasWithLimit(10), GlobalVersion: vm.MaxSupportedGlobalVersion}
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
	state = &vm.State{Stack: vm.NewStack(), Gas: vm.GasWithLimit(1), GlobalVersion: vm.MaxSupportedGlobalVersion}
	if err := noGas.Interpret(state); err == nil {
		t.Fatal("expected out of gas")
	}
	if denied != 0 {
		t.Fatalf("action should not run on gas failure")
	}
}

func TestArgOPLifecycle(t *testing.T) {
	called := 0

	op := NewArgOP(&ArgOP{
		Prefixed: SinglePrefixed(helpersPrefixForTest()),
		ArgBits:  3,
		Action: func(state *vm.State, args uint64) error {
			called++
			return state.Stack.PushInt(big.NewInt(int64(args) + 72))
		},
		Name:         func(uint64) string { return "ADVANCED" },
		BaseGasPrice: 4,
		MinVersion:   21,
	})

	if len(op.GetPrefixes()) != 1 {
		t.Fatalf("unexpected prefix count")
	}
	if op.SerializeArgsText(0x5) != "ADVANCED" {
		t.Fatalf("unexpected name: %q", op.SerializeArgsText(0x5))
	}
	if op.ArgInstructionBits(0x5) != 7 {
		t.Fatalf("unexpected instruction bits: %d", op.ArgInstructionBits(0x5))
	}
	if op.MinGlobalVersion() != 21 {
		t.Fatalf("unexpected min version: %d", op.MinGlobalVersion())
	}

	serialized := op.SerializeArgs(0x5).EndCell()
	if got := serialized.MustBeginParse().MustLoadUInt(7); got != 0b1010101 {
		t.Fatalf("unexpected serialized value: %#b", got)
	}
	decoded, err := op.DecodeArgs(nil, serialized.MustBeginParse())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded != 0x5 {
		t.Fatalf("unexpected decoded operand: %d", decoded)
	}

	state := &vm.State{Stack: vm.NewStack(), Gas: vm.GasWithLimit(10), GlobalVersion: vm.MaxSupportedGlobalVersion}
	if err := op.InterpretArgs(state, decoded); err != nil {
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

	// An opcode with no operand still round-trips through the shared instance.
	plain := NewArgOP(&ArgOP{
		Prefixed: SinglePrefixed(BytesPrefix(0x80)),
		Action:   func(*vm.State, uint64) error { return nil },
		Name:     func(uint64) string { return "PLAIN" },
	})
	if _, err := plain.DecodeArgs(nil, plain.SerializeArgs(0).EndCell().MustBeginParse()); err != nil {
		t.Fatalf("plain decode failed: %v", err)
	}

	multiPrefix := NewArgOP(&ArgOP{
		Prefixed: NewPrefixed(BytesPrefix(0x83), BytesPrefix(0x90)),
		Action:   func(*vm.State, uint64) error { return nil },
		Name:     func(uint64) string { return "MULTI" },
	})
	prefixes := multiPrefix.GetPrefixes()
	if len(prefixes) != 2 {
		t.Fatalf("unexpected multi prefix count: %d", len(prefixes))
	}
	if got := prefixes[0].MustLoadUInt(8); got != 0x83 {
		t.Fatalf("unexpected first multi prefix: %#x", got)
	}
	if got := prefixes[1].MustLoadUInt(8); got != 0x90 {
		t.Fatalf("unexpected second multi prefix: %#x", got)
	}

	// Prefixes of differing length would leave the default decoder guessing how
	// far to skip, so building such an opcode without its own Decode is refused
	// rather than silently mis-decoded.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected mixed-length prefixes without Decode to panic")
			}
		}()
		NewArgOP(&ArgOP{
			Prefixed: NewPrefixed(BytesPrefix(0x83), SlicePrefix(4, []byte{0x90})),
			Action:   func(*vm.State, uint64) error { return nil },
			Name:     func(uint64) string { return "MIXED" },
		})
	}()

	// Bind drives a shared opcode through the plain OP interface.
	bound := vm.Bind(op, 0x5)
	if bound.SerializeText() != "ADVANCED" {
		t.Fatalf("unexpected bound name: %q", bound.SerializeText())
	}
	if got := bound.Serialize().EndCell().MustBeginParse().MustLoadUInt(7); got != 0b1010101 {
		t.Fatalf("unexpected bound serialization: %#b", got)
	}
	if err := bound.Deserialize(bound.Serialize().EndCell().MustBeginParse()); err != nil {
		t.Fatalf("bound deserialize failed: %v", err)
	}

	state = &vm.State{Stack: vm.NewStack(), Gas: vm.GasWithLimit(1), GlobalVersion: vm.MaxSupportedGlobalVersion}
	failing := NewArgOP(&ArgOP{
		Prefixed:     SinglePrefixed(BytesPrefix(0x81)),
		Action:       func(*vm.State, uint64) error { t.Fatal("action should not run"); return nil },
		Name:         func(uint64) string { return "FAIL" },
		BaseGasPrice: 2,
	})
	if err := failing.InterpretArgs(state, 0); err == nil {
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
	largeQuotient := new(big.Int).Lsh(big.NewInt(1), 200)
	large := new(big.Int).Mul(largeQuotient, big.NewInt(6))
	large.Add(large, big.NewInt(3))
	largeDivisor := big.NewInt(6)
	wantLarge := new(big.Int).Set(largeQuotient)
	wantLarge.Add(wantLarge, big.NewInt(1))
	if got := DivRound(large, largeDivisor); got.Cmp(wantLarge) != 0 {
		t.Fatalf("unexpected large rounded result: got=%s want=%s", got, wantLarge)
	}

	if got := DivCeil(big.NewInt(7), big.NewInt(3)); got.Int64() != 3 {
		t.Fatalf("unexpected ceil result: %s", got)
	}
	if got := DivCeil(big.NewInt(-7), big.NewInt(3)); got.Int64() != -2 {
		t.Fatalf("unexpected negative ceil result: %s", got)
	}
}

// The padded key a truncated instruction selects decides how much gas the
// reference VM charges before throwing inv_opcode, so the arithmetic is pinned
// here together with the fact that the remainder is always consumed.
func TestPeekZeroPaddedOpcode(t *testing.T) {
	t.Run("PadsAndDrainsRemainder", func(t *testing.T) {
		tests := []struct {
			name string
			raw  uint64
			rest uint
			bits uint
			want uint64
		}{
			{name: "EmptyByte", bits: 8},
			{name: "SingleBitByte", raw: 0b1, rest: 1, bits: 8, want: 0x80},
			{name: "SevenBitsByte", raw: 0b1000001, rest: 7, bits: 8, want: 0x82},
			{name: "EmptyNibble", bits: 4},
			{name: "SingleBitNibble", raw: 0b1, rest: 1, bits: 4, want: 0b1000},
			{name: "ThreeBitsNibble", raw: 0b011, rest: 3, bits: 4, want: 0b0110},
			{name: "EmptyTriple", bits: 3},
			{name: "TwoBitsTriple", raw: 0b11, rest: 2, bits: 3, want: 0b110},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				code := cell.BeginCell().MustStoreUInt(tt.raw, tt.rest).EndCell().MustBeginParse()
				if got := PeekZeroPaddedOpcode(code, tt.bits); got != tt.want {
					t.Fatalf("unexpected padded opcode: got %#b want %#b", got, tt.want)
				}
				if left := code.BitsLeft(); left != 0 {
					t.Fatalf("remainder must be consumed, %d bits left", left)
				}
			})
		}
	})

	// Padding only ever appends zeros, so an opcode-table key whose lowest bit is
	// set can never be selected by a truncated instruction.
	t.Run("TrailingOneKeysAreUnreachable", func(t *testing.T) {
		for rest := uint(0); rest < 8; rest++ {
			for raw := uint64(0); raw < 1<<rest; raw++ {
				code := cell.BeginCell().MustStoreUInt(raw, rest).EndCell().MustBeginParse()
				got := PeekZeroPaddedOpcode(code, 8)
				if want := raw << (8 - rest); got != want {
					t.Fatalf("raw %#b of %d bits: got %#x want %#x", raw, rest, got, want)
				}
				if got&1 != 0 {
					t.Fatalf("raw %#b of %d bits padded to odd key %#x", raw, rest, got)
				}
			}
		}
	})
}

func helpersPrefixForTest() BitPrefix {
	return SlicePrefix(4, []byte{0xA0})
}
