package cellslice

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestLittleEndianHelperEdges(t *testing.T) {
	u64 := []byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}
	if got := decodeLEInt(u64, true); got.Uint64() != 0x1122334455667788 {
		t.Fatalf("decode unsigned 64 = %x, want 1122334455667788", got.Uint64())
	}

	if got := decodeLEInt([]byte{0xFE, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, false); got.Int64() != -2 {
		t.Fatalf("decode signed 64 = %d, want -2", got.Int64())
	}

	if !fitsSignedBits(big.NewInt(0), 0) || fitsSignedBits(big.NewInt(1), 0) || fitsSignedBits(big.NewInt(-1), 0) {
		t.Fatal("unexpected zero-width signed fit rule")
	}

	encoded, err := encodeLEInt(new(big.Int).SetUint64(^uint64(0)), 8, true)
	if err != nil {
		t.Fatalf("encode max unsigned 64 failed: %v", err)
	}
	if binary.LittleEndian.Uint64(encoded) != ^uint64(0) {
		t.Fatalf("encoded max unsigned 64 = %x", binary.LittleEndian.Uint64(encoded))
	}

	tooLargeUnsigned := new(big.Int).Lsh(big.NewInt(1), 64)
	assertCellSliceVMErrorCode(t, firstLittleEndianEncodeErr(tooLargeUnsigned, 8, true), vmerr.CodeRangeCheck)
	assertCellSliceVMErrorCode(t, firstLittleEndianEncodeErr(big.NewInt(-1), 8, true), vmerr.CodeRangeCheck)
	assertCellSliceVMErrorCode(t, firstLittleEndianEncodeErr(new(big.Int).Lsh(big.NewInt(1), 63), 8, false), vmerr.CodeRangeCheck)
}

func TestLittleEndianLoadStoreStackTypeAndQuietEdges(t *testing.T) {
	t.Run("LoadStackAndType", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, LDILE4().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDULE8().Interpret(st), vmerr.CodeTypeCheck)

		if err := LDILE4().Deserialize(cell.BeginCell().MustStoreUInt(0xD75, 12).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short little-endian load suffix unexpectedly decoded")
		}
	})

	t.Run("LoadUnderflowAndQuietSuccessShapes", func(t *testing.T) {
		short := cell.BeginCell().MustStoreSlice([]byte{0xAA, 0xBB, 0xCC, 0xDD}, 31).EndCell().MustBeginParse()

		st := newCellSliceState()
		pushCellSliceSlice(t, st, short.Copy())
		assertCellSliceVMErrorCode(t, LDILE4().Interpret(st), vmerr.CodeCellUnderflow)

		src := cell.BeginCell().MustStoreSlice([]byte{0x78, 0x56, 0x34, 0x12, 0xA0}, 36).EndCell().MustBeginParse()
		st = newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		if err := LDULE4Q().Interpret(st); err != nil {
			t.Fatalf("LDULE4Q success failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("LDULE4Q success flag = false")
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 4 || rest.MustLoadUInt(4) != 0xA {
			t.Fatal("LDULE4Q success remainder mismatch")
		}
		if got, err := st.Stack.PopIntFinite(); err != nil {
			t.Fatalf("pop LDULE4Q value: %v", err)
		} else if got.Uint64() != 0x12345678 {
			t.Fatalf("LDULE4Q value = %x, want 12345678", got.Uint64())
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		if err := PLDILE4Q().Interpret(st); err != nil {
			t.Fatalf("PLDILE4Q success failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("PLDILE4Q success flag = false")
		}
		if got, err := st.Stack.PopIntFinite(); err != nil {
			t.Fatalf("pop PLDILE4Q value: %v", err)
		} else if got.Int64() != 0x12345678 {
			t.Fatalf("PLDILE4Q value = %x, want 12345678", got.Int64())
		}
	})

	t.Run("QuietUnderflowShapes", func(t *testing.T) {
		short := cell.BeginCell().MustStoreSlice([]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22}, 63).EndCell().MustBeginParse()

		st := newCellSliceState()
		pushCellSliceSlice(t, st, short.Copy())
		if err := LDULE8Q().Interpret(st); err != nil {
			t.Fatalf("LDULE8Q short load failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); ok {
			t.Fatal("LDULE8Q short load flag = true")
		}
		if preserved := popCellSliceSlice(t, st); preserved.BitsLeft() != 63 {
			t.Fatalf("LDULE8Q preserved bits = %d, want 63", preserved.BitsLeft())
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, short.Copy())
		if err := PLDULE8Q().Interpret(st); err != nil {
			t.Fatalf("PLDULE8Q short preload failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); ok {
			t.Fatal("PLDULE8Q short preload flag = true")
		}
		if st.Stack.Len() != 0 {
			t.Fatalf("PLDULE8Q short preload stack depth = %d, want 0", st.Stack.Len())
		}
	})

	t.Run("LoadResultStackOverflowBranches", func(t *testing.T) {
		src := cell.BeginCell().MustStoreSlice([]byte{0x78, 0x56, 0x34, 0x12, 0xA0}, 36).EndCell().MustBeginParse()

		st := newCellSliceState()
		fillLoadRefGramsStack(t, st, 1)
		pushCellSliceSlice(t, st, src.Copy())
		assertCellSliceVMErrorCode(t, LDULE4().Interpret(st), vmerr.CodeStackOverflow)
		if got, err := st.Stack.PopIntFinite(); err != nil {
			t.Fatalf("pop partial LDULE4 value: %v", err)
		} else if got.Uint64() != 0x12345678 {
			t.Fatalf("partial LDULE4 value = %x, want 12345678", got.Uint64())
		}

		st = newCellSliceState()
		fillLoadRefGramsStack(t, st, 2)
		pushCellSliceSlice(t, st, src.Copy())
		assertCellSliceVMErrorCode(t, LDULE4Q().Interpret(st), vmerr.CodeStackOverflow)
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 4 || rest.MustLoadUInt(4) != 0xA {
			t.Fatal("partial LDULE4Q rest mismatch")
		}
		if got, err := st.Stack.PopIntFinite(); err != nil {
			t.Fatalf("pop partial LDULE4Q value: %v", err)
		} else if got.Uint64() != 0x12345678 {
			t.Fatalf("partial LDULE4Q value = %x, want 12345678", got.Uint64())
		}

		st = newCellSliceState()
		fillLoadRefGramsStack(t, st, 1)
		pushCellSliceSlice(t, st, src.Copy())
		assertCellSliceVMErrorCode(t, PLDULE4Q().Interpret(st), vmerr.CodeStackOverflow)
		if got, err := st.Stack.PopIntFinite(); err != nil {
			t.Fatalf("pop partial PLDULE4Q value: %v", err)
		} else if got.Uint64() != 0x12345678 {
			t.Fatalf("partial PLDULE4Q value = %x, want 12345678", got.Uint64())
		}
	})

	t.Run("StoreStackTypeRangeAndOverflow", func(t *testing.T) {
		if err := STILE4().Deserialize(cell.BeginCell().MustStoreUInt(0xCF28>>2, 14).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short STILE suffix unexpectedly decoded")
		}

		st := newCellSliceState()
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, STILE4().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceInt(t, st, 1)
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, STULE4().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STILE8().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		if err := st.Stack.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("push NaN: %v", err)
		}
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STILE4().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceInt(t, st, -1)
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STULE8().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceBigInt(t, st, new(big.Int).Lsh(big.NewInt(1), 63))
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STILE8().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceInt(t, st, -1)
		pushCellSliceBuilder(t, st, cell.BeginCell().MustStoreSlice(make([]byte, 120), 960))
		assertCellSliceVMErrorCode(t, STULE8().Interpret(st), vmerr.CodeCellOverflow)
	})
}

func FuzzTVMLittleEndianEncodeDecodeRoundTrip(f *testing.F) {
	for _, seed := range []struct {
		bytes    byte
		unsigned bool
		raw      int64
	}{
		{bytes: 4, raw: -2},
		{bytes: 4, unsigned: true, raw: 0x7FFFFFFF},
		{bytes: 4, unsigned: true, raw: -1},
		{bytes: 8, raw: -2},
		{bytes: 8, unsigned: true, raw: 1<<63 - 1},
		{bytes: 8, raw: 1 << 62},
	} {
		f.Add(seed.bytes, seed.unsigned, seed.raw)
	}

	f.Fuzz(func(t *testing.T, rawBytes byte, unsigned bool, raw int64) {
		bytesLen := 4
		if rawBytes%2 != 0 {
			bytesLen = 8
		}

		x := littleEndianFuzzValue(raw, bytesLen, unsigned)
		data, err := encodeLEInt(x, bytesLen, unsigned)
		bits := uint(bytesLen * 8)
		fits := fitsSignedBits(x, bits)
		if unsigned {
			fits = fitsUnsignedBits(x, bits)
		}
		if !fits {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
			return
		}
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}

		got := decodeLEInt(data, unsigned)
		if got.Cmp(x) != 0 {
			t.Fatalf("roundtrip bytes=%d unsigned=%v got=%s want=%s data=%x", bytesLen, unsigned, got, x, data)
		}
	})
}

func firstLittleEndianEncodeErr(x *big.Int, bytes int, unsigned bool) error {
	_, err := encodeLEInt(x, bytes, unsigned)
	return err
}

func littleEndianFuzzValue(raw int64, bytesLen int, unsigned bool) *big.Int {
	if raw%29 == 0 {
		if unsigned {
			return new(big.Int).Lsh(big.NewInt(1), uint(bytesLen*8))
		}
		return new(big.Int).Lsh(big.NewInt(1), uint(bytesLen*8-1))
	}
	if unsigned && raw < 0 {
		return big.NewInt(raw)
	}
	if bytesLen == 4 {
		if unsigned {
			return new(big.Int).SetUint64(uint64(uint32(raw)))
		}
		return big.NewInt(int64(int32(raw)))
	}
	return big.NewInt(raw)
}
