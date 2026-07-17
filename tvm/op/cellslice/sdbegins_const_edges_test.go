package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestSDBeginsConstSerializeDeserializeEdges(t *testing.T) {
	t.Run("NilValueMatchesEmptyPrefix", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		if err := SDBEGINSCONST(nil, true).Interpret(st); err != nil {
			t.Fatalf("empty quiet SDBEGINSCONST failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("empty quiet prefix should match")
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 8 || rest.MustLoadUInt(8) != 0xAB {
			t.Fatal("empty prefix should preserve source slice")
		}
	})

	t.Run("QuietRoundTripKeepsModeAndValue", func(t *testing.T) {
		value := cell.BeginCell().MustStoreUInt(0b1011001101, 10).ToSlice()
		op := SDBEGINSCONST(value, true)
		decoded := SDBEGINSCONST(nil, false)
		if err := decoded.Deserialize(op.Serialize().EndCell().MustBeginParse()); err != nil {
			t.Fatalf("quiet deserialize failed: %v", err)
		}
		if got := decoded.SerializeText(); got == "" || got[:10] != "SDBEGINSQ " {
			t.Fatalf("unexpected quiet text: %q", got)
		}
		if got := decoded.InstructionBits(); got != 21 {
			t.Fatalf("instruction bits = %d, want 21", got)
		}

		st := newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0b101100110111, 12).ToSlice())
		if err := decoded.Interpret(st); err != nil {
			t.Fatalf("quiet decoded interpret failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("quiet decoded prefix should match")
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 2 || rest.MustLoadUInt(2) != 0b11 {
			t.Fatal("quiet decoded prefix remainder mismatch")
		}
	})

	t.Run("ShortAndMalformedDeserialize", func(t *testing.T) {
		if err := SDBEGINSCONST(nil, false).Deserialize(cell.BeginCell().MustStoreUInt(0, 12).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short SDBEGINSCONST prefix unexpectedly decoded")
		}
		assertCellSliceVMErrorCode(t, SDBEGINSCONST(nil, false).Deserialize(cell.BeginCell().
			MustStoreUInt(0xD728>>3, 13).
			EndCell().MustBeginParse()), vmerr.CodeInvalidOpcode)
		assertCellSliceVMErrorCode(t, SDBEGINSCONST(nil, false).Deserialize(cell.BeginCell().
			MustStoreUInt(0xD728>>3, 13).
			MustStoreUInt(0, 8).
			EndCell().MustBeginParse()), vmerr.CodeInvalidOpcode)
	})

	t.Run("PayloadHelpersNoPaddingBranch", func(t *testing.T) {
		if got := paddedBeginsConstBits(2); got != 3 {
			t.Fatalf("padded bits = %d, want 3", got)
		}
		payload := encodeBeginsConstPayload(cell.BeginCell().MustStoreUInt(0b101, 3).ToSlice(), 3).
			EndCell().MustBeginParse()
		if payload.BitsLeft() != 3 || payload.MustLoadUInt(3) != 0b101 {
			t.Fatal("payload should be unchanged when totalBits equals actual bits")
		}
	})
}

func TestSDBeginsConstInterpretEdges(t *testing.T) {
	needle := cell.BeginCell().MustStoreUInt(0b101, 3).ToSlice()

	t.Run("StackTypeAndNonQuietExact", func(t *testing.T) {
		st := newCellSliceState()
		assertCellSliceVMErrorCode(t, SDBEGINSCONST(needle, false).Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, SDBEGINSCONST(needle, false).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0b101, 3).ToSlice())
		if err := SDBEGINSCONST(needle, false).Interpret(st); err != nil {
			t.Fatalf("exact non-quiet prefix failed: %v", err)
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 0 {
			t.Fatalf("exact prefix remainder bits = %d, want 0", rest.BitsLeft())
		}
	})

	t.Run("QuietSuccessAndFailureShapes", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0b10111, 5).ToSlice())
		if err := SDBEGINSCONST(needle, true).Interpret(st); err != nil {
			t.Fatalf("quiet prefix success failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("quiet prefix success flag = false")
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 2 || rest.MustLoadUInt(2) != 0b11 {
			t.Fatal("quiet prefix success remainder mismatch")
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0b00111, 5).ToSlice())
		if err := SDBEGINSCONST(needle, true).Interpret(st); err != nil {
			t.Fatalf("quiet prefix failure failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); ok {
			t.Fatal("quiet prefix failure flag = true")
		}
		if preserved := popCellSliceSlice(t, st); preserved.BitsLeft() != 5 || preserved.MustLoadUInt(5) != 0b00111 {
			t.Fatal("quiet prefix failure should preserve source slice")
		}
	})
}

func FuzzTVMSDBeginsConstRules(f *testing.F) {
	for _, seed := range []struct {
		prefixLen byte
		tailLen   byte
		prefix    uint64
		tail      uint64
		quiet     bool
		hit       bool
	}{
		{prefixLen: 0, tailLen: 8, quiet: false, hit: true},
		{prefixLen: 3, tailLen: 2, prefix: 0b101, tail: 0b11, quiet: false, hit: true},
		{prefixLen: 3, tailLen: 2, prefix: 0b101, tail: 0b11, quiet: false, hit: false},
		{prefixLen: 3, tailLen: 2, prefix: 0b101, tail: 0b11, quiet: true, hit: false},
		{prefixLen: 16, tailLen: 16, prefix: 0xA55A, tail: 0x5AA5, quiet: true, hit: true},
	} {
		f.Add(seed.prefixLen, seed.tailLen, seed.prefix, seed.tail, seed.quiet, seed.hit)
	}

	f.Fuzz(func(t *testing.T, rawPrefixLen, rawTailLen byte, rawPrefix, rawTail uint64, quiet, hit bool) {
		prefixLen := uint(rawPrefixLen % 17)
		tailLen := uint(rawTailLen % 17)
		prefix := sdbeginsConstBitsSlice(prefixLen, rawPrefix)

		sourcePrefix := rawPrefix
		if !hit && prefixLen > 0 {
			sourcePrefix ^= uint64(1) << (prefixLen - 1)
		}
		source := sdbeginsConstCombinedSlice(prefixLen, sourcePrefix, tailLen, rawTail)

		st := newCellSliceState()
		pushCellSliceSlice(t, st, source)
		err := SDBEGINSCONST(prefix, quiet).Interpret(st)

		wantHit := hit || prefixLen == 0
		if !wantHit {
			if quiet {
				if err != nil {
					t.Fatalf("quiet miss failed: %v", err)
				}
				if ok := popCellSliceBool(t, st); ok {
					t.Fatal("quiet miss flag = true")
				}
				if preserved := popCellSliceSlice(t, st); preserved.BitsLeft() != prefixLen+tailLen {
					t.Fatalf("quiet miss preserved bits = %d, want %d", preserved.BitsLeft(), prefixLen+tailLen)
				}
				return
			}
			assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
			return
		}

		if err != nil {
			t.Fatalf("prefix hit failed: %v", err)
		}
		if quiet {
			if ok := popCellSliceBool(t, st); !ok {
				t.Fatal("quiet hit flag = false")
			}
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != tailLen {
			t.Fatalf("hit remainder bits = %d, want %d", rest.BitsLeft(), tailLen)
		}
	})
}

func sdbeginsConstCombinedSlice(prefixLen uint, prefix uint64, tailLen uint, tail uint64) *cell.Slice {
	b := cell.BeginCell()
	storeSDBeginsConstBits(b, prefixLen, prefix)
	storeSDBeginsConstBits(b, tailLen, tail)
	return b.ToSlice()
}

func sdbeginsConstBitsSlice(bits uint, value uint64) *cell.Slice {
	b := cell.BeginCell()
	storeSDBeginsConstBits(b, bits, value)
	return b.ToSlice()
}

func storeSDBeginsConstBits(b *cell.Builder, bits uint, value uint64) {
	for i := uint(0); i < bits; i++ {
		shift := bits - 1 - i
		b.MustStoreUInt((value>>shift)&1, 1)
	}
}
