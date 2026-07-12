package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestPUSHINTTinyFormMatchesReferenceRange(t *testing.T) {
	for _, tc := range []struct {
		value int64
		byte  uint64
	}{
		{value: 0, byte: 0x70},
		{value: 3, byte: 0x73},
		{value: 9, byte: 0x79},
		{value: 10, byte: 0x7A},
		{value: -5, byte: 0x7B},
		{value: -4, byte: 0x7C},
		{value: -1, byte: 0x7F},
	} {
		op := PUSHINT(big.NewInt(tc.value))
		if bits := op.InstructionBits(); bits != 8 {
			t.Fatalf("unexpected instruction size for %d: %d", tc.value, bits)
		}

		want := cell.BeginCell().MustStoreUInt(tc.byte, 8).EndCell().Dump()
		if got := op.Serialize().EndCell().Dump(); got != want {
			t.Fatalf("unexpected encoding for %d: got %s want %s", tc.value, got, want)
		}

		slice := cell.BeginCell().MustStoreUInt(tc.byte, 8).EndCell().MustBeginParse()
		decoded := PUSHINT(nil)
		if err := decoded.Deserialize(slice); err != nil {
			t.Fatalf("failed to decode %d: %v", tc.value, err)
		}
		if decoded.value.Int64() != tc.value {
			t.Fatalf("unexpected decoded value for %d: got %d", tc.value, decoded.value.Int64())
		}
	}
}

func TestPUSHINTTinyFormSharedImmediatesStayImmutable(t *testing.T) {
	st := vm.NewStack()

	for prefix := uint64(0x70); prefix <= 0x7F; prefix++ {
		want := int64(((prefix + 5) & 0xF) - 5)

		op := PUSHINT(nil)
		code := cell.BeginCell().MustStoreUInt(prefix, 8).EndCell().MustBeginParse()
		if err := op.Deserialize(code); err != nil {
			t.Fatalf("failed to decode 0x%x: %v", prefix, err)
		}
		if op.value.Int64() != want {
			t.Fatalf("unexpected decoded value for 0x%x: got %d want %d", prefix, op.value.Int64(), want)
		}

		if err := op.Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("failed to interpret 0x%x: %v", prefix, err)
		}
		popped, err := st.PopInt()
		if err != nil {
			t.Fatalf("failed to pop for 0x%x: %v", prefix, err)
		}
		if popped.Int64() != want {
			t.Fatalf("unexpected stack value for 0x%x: got %d want %d", prefix, popped.Int64(), want)
		}

		encoded := op.Serialize().EndCell()
		wantCell := cell.BeginCell().MustStoreUInt(prefix, 8).EndCell()
		if encoded.Dump() != wantCell.Dump() {
			t.Fatalf("unexpected re-encoding for 0x%x: got %s want %s", prefix, encoded.Dump(), wantCell.Dump())
		}
	}

	// after all interpret/serialize round-trips the shared immediates must be intact
	for i, val := range pushIntSmallImmediates {
		want := int64(((i + 5) & 0xF) - 5)
		if val.Int64() != want {
			t.Fatalf("shared immediate %d mutated: got %d want %d", i, val.Int64(), want)
		}
	}
}

func TestPUSHINTFallsBackFromTinyFormAfterTen(t *testing.T) {
	op := PUSHINT(big.NewInt(11))
	if bits := op.InstructionBits(); bits != 16 {
		t.Fatalf("unexpected instruction size for 11: %d", bits)
	}

	want := cell.BeginCell().MustStoreUInt(0x80, 8).MustStoreBigInt(big.NewInt(11), 8).EndCell().Dump()
	if got := op.Serialize().EndCell().Dump(); got != want {
		t.Fatalf("unexpected encoding for 11: got %s want %s", got, want)
	}
}
