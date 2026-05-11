package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
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

		slice := cell.BeginCell().MustStoreUInt(tc.byte, 8).EndCell().BeginParse()
		decoded := PUSHINT(nil)
		if err := decoded.Deserialize(slice); err != nil {
			t.Fatalf("failed to decode %d: %v", tc.value, err)
		}
		if decoded.value.Int64() != tc.value {
			t.Fatalf("unexpected decoded value for %d: got %d", tc.value, decoded.value.Int64())
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
