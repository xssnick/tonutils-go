package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestBTOSLDREFLDGRAMSEdges(t *testing.T) {
	t.Run("BTOSStackTypeAndVersion", func(t *testing.T) {
		op := BTOS()
		if got := op.MinGlobalVersion(); got != 12 {
			t.Fatalf("BTOS min version = %d, want 12", got)
		}

		assertCellSliceVMErrorCode(t, op.Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, op.Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("LDREFStackTypeUnderflowAndSuccess", func(t *testing.T) {
		ref := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()

		assertCellSliceVMErrorCode(t, LDREF().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDREF().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xF, 4).EndCell().MustBeginParse())
		assertCellSliceVMErrorCode(t, LDREF().Interpret(st), vmerr.CodeCellUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xB, 4).MustStoreRef(ref).EndCell().MustBeginParse())
		if err := LDREF().Interpret(st); err != nil {
			t.Fatalf("LDREF success failed: %v", err)
		}
		rest := popCellSliceSlice(t, st)
		if rest.BitsLeft() != 4 || rest.RefsNum() != 0 || rest.MustLoadUInt(4) != 0xB {
			t.Fatal("LDREF remainder mismatch")
		}
		if got := popCellSliceCell(t, st); !sameCellHash(got, ref) {
			t.Fatal("LDREF loaded ref mismatch")
		}
	})

	t.Run("LDGRAMSStackTypeMalformedAndSuccess", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, LDGRAMS().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDGRAMS().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(1, 4).EndCell().MustBeginParse())
		if err := LDGRAMS().Interpret(st); err == nil {
			t.Fatal("LDGRAMS malformed coin encoding unexpectedly succeeded")
		}

		want := new(big.Int).SetUint64(0x112233445566)
		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreBigCoins(want).EndCell().MustBeginParse())
		if err := LDGRAMS().Interpret(st); err != nil {
			t.Fatalf("LDGRAMS success failed: %v", err)
		}
		rest := popCellSliceSlice(t, st)
		if rest.BitsLeft() != 0 || rest.RefsNum() != 0 {
			t.Fatalf("LDGRAMS rest=(%d,%d), want empty", rest.BitsLeft(), rest.RefsNum())
		}
		got, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop LDGRAMS value: %v", err)
		}
		if got.Cmp(want) != 0 {
			t.Fatalf("LDGRAMS value = %s, want %s", got, want)
		}
	})
}

func FuzzTVMLDREFAndLDGRAMSRules(f *testing.F) {
	for _, seed := range []struct {
		group byte
		bits  byte
		refs  byte
		coins uint64
	}{
		{group: 0, bits: 4, refs: 0},
		{group: 0, bits: 4, refs: 1},
		{group: 0, bits: 8, refs: 4},
		{group: 1, coins: 0},
		{group: 1, coins: 0x112233},
	} {
		f.Add(seed.group, seed.bits, seed.refs, seed.coins)
	}

	f.Fuzz(func(t *testing.T, group, rawBits, rawRefs byte, rawCoins uint64) {
		if group%2 == 0 {
			bits := uint(rawBits % 33)
			refs := int(rawRefs % 5)
			st := newCellSliceState()
			pushCellSliceSlice(t, st, loadRefGramsSlice(bits, refs))
			err := LDREF().Interpret(st)
			if refs == 0 {
				assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
				return
			}
			if err != nil {
				t.Fatalf("LDREF failed: %v", err)
			}
			rest := popCellSliceSlice(t, st)
			if rest.BitsLeft() != bits || rest.RefsNum() != refs-1 {
				t.Fatalf("LDREF rest=(%d,%d), want=(%d,%d)", rest.BitsLeft(), rest.RefsNum(), bits, refs-1)
			}
			_ = popCellSliceCell(t, st)
			return
		}

		coins := new(big.Int).SetUint64(rawCoins % (1 << 40))
		st := newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreBigCoins(coins).EndCell().MustBeginParse())
		if err := LDGRAMS().Interpret(st); err != nil {
			t.Fatalf("LDGRAMS failed: %v", err)
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 0 || rest.RefsNum() != 0 {
			t.Fatalf("LDGRAMS rest=(%d,%d), want empty", rest.BitsLeft(), rest.RefsNum())
		}
		got, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop LDGRAMS value: %v", err)
		}
		if got.Cmp(coins) != 0 {
			t.Fatalf("LDGRAMS value = %s, want %s", got, coins)
		}
	})
}

func loadRefGramsSlice(bits uint, refs int) *cell.Slice {
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreSlice(make([]byte, (bits+7)/8), bits)
	}
	for i := 0; i < refs; i++ {
		b.MustStoreRef(cell.BeginCell().EndCell())
	}
	return b.EndCell().MustBeginParse()
}
