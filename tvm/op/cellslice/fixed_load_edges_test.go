package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestFixedLoadLDUActionEdges(t *testing.T) {
	t.Run("DeserializeShortSuffix", func(t *testing.T) {
		if err := LDU(1).Deserialize(cell.BeginCell().MustStoreUInt(0xD3, 8).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short LDU suffix unexpectedly decoded")
		}
	})

	t.Run("Uint64MaxIntStaysSmall", func(t *testing.T) {
		const maxInt63 = uint64(1<<63 - 1)
		st := newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreBigUInt(new(big.Int).SetUint64(maxInt63), 64).ToSlice())
		if err := LDU(64).Interpret(st); err != nil {
			t.Fatalf("LDU 64 max int failed: %v", err)
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 0 {
			t.Fatalf("LDU 64 rest bits = %d, want 0", rest.BitsLeft())
		}
		if got := popCellSliceInt(t, st); got != int64(maxInt63) {
			t.Fatalf("LDU 64 value = %d, want %d", got, maxInt63)
		}
	})

	t.Run("AboveUint64UsesBigIntPath", func(t *testing.T) {
		want := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(5))
		st := newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreBigUInt(want, 65).ToSlice())
		if err := LDU(65).Interpret(st); err != nil {
			t.Fatalf("LDU 65 failed: %v", err)
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 0 {
			t.Fatalf("LDU 65 rest bits = %d, want 0", rest.BitsLeft())
		}
		got, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop LDU 65 value: %v", err)
		}
		if got.Cmp(want) != 0 {
			t.Fatalf("LDU 65 value = %s, want %s", got, want)
		}
	})

	t.Run("AboveUint64Underflow", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreBigUInt(new(big.Int).SetUint64(^uint64(0)), 64).ToSlice())
		if err := LDU(65).Interpret(st); err == nil {
			t.Fatal("LDU 65 underflow unexpectedly succeeded")
		}
	})

	t.Run("StackTypeAndCellUnderflow", func(t *testing.T) {
		st := newCellSliceState()
		assertCellSliceVMErrorCode(t, LDU(8).Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDU(8).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xF, 4).ToSlice())
		if err := LDU(8).Interpret(st); err == nil {
			t.Fatal("LDU underflow unexpectedly succeeded")
		}
	})
}

func TestFixedLoadLDIAndPLDUEdges(t *testing.T) {
	t.Run("LDIStackTypeDeserializeAndUnderflow", func(t *testing.T) {
		if err := LDI(1).Deserialize(cell.BeginCell().MustStoreUInt(0xD2, 8).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short LDI suffix unexpectedly decoded")
		}

		assertCellSliceVMErrorCode(t, LDI(8).Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDI(8).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xF, 4).ToSlice())
		if err := LDI(8).Interpret(st); err == nil {
			t.Fatal("LDI underflow unexpectedly succeeded")
		}
	})

	t.Run("LDIPushRestStackOverflowLeavesValue", func(t *testing.T) {
		st := newCellSliceState()
		fillLoadRefGramsStack(t, st, 1)
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		assertCellSliceVMErrorCode(t, LDI(8).Interpret(st), vmerr.CodeStackOverflow)

		if got, err := st.Stack.PopIntFinite(); err != nil {
			t.Fatalf("pop partial LDI value: %v", err)
		} else if got.Int64() != -85 {
			t.Fatalf("partial LDI value = %d, want -85", got.Int64())
		}
	})

	t.Run("PLDUStackTypeDeserializeAndUnderflow", func(t *testing.T) {
		if err := PLDU(1).Deserialize(cell.BeginCell().MustStoreUInt(0xD70B, 16).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short PLDU suffix unexpectedly decoded")
		}

		assertCellSliceVMErrorCode(t, PLDU(8).Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, PLDU(8).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xF, 4).ToSlice())
		if err := PLDU(8).Interpret(st); err == nil {
			t.Fatal("PLDU underflow unexpectedly succeeded")
		}
	})
}

func TestFixedSliceLoadAndSkipEdges(t *testing.T) {
	t.Run("LDSLICEXSuccessOrder", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, st, 4)
		if err := LDSLICEX().Interpret(st); err != nil {
			t.Fatalf("LDSLICEX failed: %v", err)
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 4 || rest.MustLoadUInt(4) != 0xB {
			t.Fatal("LDSLICEX rest mismatch")
		}
		if part := popCellSliceSlice(t, st); part.BitsLeft() != 4 || part.MustLoadUInt(4) != 0xA {
			t.Fatal("LDSLICEX part mismatch")
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, st, 0)
		if err := LDSLICEX().Interpret(st); err != nil {
			t.Fatalf("zero-width LDSLICEX failed: %v", err)
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 8 || rest.MustLoadUInt(8) != 0xAB {
			t.Fatal("zero-width LDSLICEX rest mismatch")
		}
		if part := popCellSliceSlice(t, st); part.BitsLeft() != 0 {
			t.Fatalf("zero-width LDSLICEX part bits = %d, want 0", part.BitsLeft())
		}
	})

	t.Run("LDSLICEXErrors", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, LDSLICEX().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAA))
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, LDSLICEX().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAA))
		pushCellSliceInt(t, st, -1)
		assertCellSliceVMErrorCode(t, LDSLICEX().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, LDSLICEX().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAA))
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDSLICEX().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 4, 0xF0))
		pushCellSliceInt(t, st, 5)
		assertCellSliceVMErrorCode(t, LDSLICEX().Interpret(st), vmerr.CodeCellUnderflow)
	})

	t.Run("SDSKIPFIRSTEdges", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, SDSKIPFIRST().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, st, 0)
		if err := SDSKIPFIRST().Interpret(st); err != nil {
			t.Fatalf("zero-width SDSKIPFIRST failed: %v", err)
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 8 || rest.MustLoadUInt(8) != 0xAB {
			t.Fatal("zero-width SDSKIPFIRST should preserve slice")
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAA))
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, SDSKIPFIRST().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 8, 0xAA))
		pushCellSliceInt(t, st, -1)
		assertCellSliceVMErrorCode(t, SDSKIPFIRST().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, SDSKIPFIRST().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, 4, 0xF0))
		pushCellSliceInt(t, st, 5)
		assertCellSliceVMErrorCode(t, SDSKIPFIRST().Interpret(st), vmerr.CodeCellUnderflow)
	})
}

func FuzzTVMFixedSliceLoadAndSkipRules(f *testing.F) {
	for _, seed := range []struct {
		op   byte
		have uint16
		want uint16
	}{
		{op: 0, have: 8, want: 4},
		{op: 0, have: 8, want: 0},
		{op: 0, have: 4, want: 5},
		{op: 0, have: 1023, want: 1024},
		{op: 1, have: 8, want: 4},
		{op: 1, have: 8, want: 0},
		{op: 1, have: 4, want: 5},
		{op: 1, have: 1023, want: 1024},
	} {
		f.Add(seed.op, seed.have, seed.want)
	}

	f.Fuzz(func(t *testing.T, rawOp byte, rawHave, rawWant uint16) {
		have := uint(rawHave % 1024)
		want := int64(rawWant % 1030)
		st := newCellSliceState()
		pushCellSliceSlice(t, st, loadFamilyBitsSlice(t, have, 0xA5))
		pushCellSliceInt(t, st, want)

		if rawOp%2 == 0 {
			err := LDSLICEX().Interpret(st)
			if want > 1023 {
				assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
				return
			}
			if uint(want) > have {
				assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
				return
			}
			if err != nil {
				t.Fatalf("LDSLICEX have=%d want=%d failed: %v", have, want, err)
			}
			if rest := popCellSliceSlice(t, st); rest.BitsLeft() != have-uint(want) {
				t.Fatalf("LDSLICEX rest bits = %d, want %d", rest.BitsLeft(), have-uint(want))
			}
			if part := popCellSliceSlice(t, st); part.BitsLeft() != uint(want) {
				t.Fatalf("LDSLICEX part bits = %d, want %d", part.BitsLeft(), want)
			}
			return
		}

		err := SDSKIPFIRST().Interpret(st)
		if want > 1023 {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
			return
		}
		if uint(want) > have {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
			return
		}
		if err != nil {
			t.Fatalf("SDSKIPFIRST have=%d want=%d failed: %v", have, want, err)
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != have-uint(want) {
			t.Fatalf("SDSKIPFIRST rest bits = %d, want %d", rest.BitsLeft(), have-uint(want))
		}
	})
}
