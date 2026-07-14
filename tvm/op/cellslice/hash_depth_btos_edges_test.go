package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestHashDepthAndBTOSStackEdges(t *testing.T) {
	t.Run("HASHCUStackType", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, HASHCU().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, HASHCU().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("HASHSUStackType", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, HASHSU().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, HASHSU().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("CDEPTHStackType", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, CDEPTH().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, CDEPTH().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("BTOSDepthOverflow", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, builderWithDeepUncheckedRef(t))
		assertCellSliceVMErrorCode(t, BTOS().Interpret(st), vmerr.CodeCellOverflow)
	})
}

func FuzzTVMHashAndDepthOps(f *testing.F) {
	for _, seed := range []struct {
		bits byte
		refs byte
		fill byte
	}{
		{bits: 0, refs: 0, fill: 0},
		{bits: 1, refs: 0, fill: 0x80},
		{bits: 8, refs: 1, fill: 0xA5},
		{bits: 63, refs: 4, fill: 0x5A},
	} {
		f.Add(seed.bits, seed.refs, seed.fill)
	}

	f.Fuzz(func(t *testing.T, rawBits, rawRefs, fill byte) {
		bits := uint(rawBits % 128)
		refs := int(rawRefs % 5)
		cl := hashDepthFuzzCell(t, bits, refs, fill)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cl)
		if err := HASHCU().Interpret(st); err != nil {
			t.Fatalf("HASHCU bits=%d refs=%d failed: %v", bits, refs, err)
		}
		if got, err := st.Stack.PopIntFinite(); err != nil {
			t.Fatalf("pop HASHCU result: %v", err)
		} else if got.Cmp(new(big.Int).SetBytes(cl.Hash())) != 0 {
			t.Fatalf("HASHCU bits=%d refs=%d mismatch", bits, refs)
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cl.MustBeginParse())
		if err := HASHSU().Interpret(st); err != nil {
			t.Fatalf("HASHSU bits=%d refs=%d failed: %v", bits, refs, err)
		}
		if got, err := st.Stack.PopIntFinite(); err != nil {
			t.Fatalf("pop HASHSU result: %v", err)
		} else if got.Cmp(new(big.Int).SetBytes(cl.Hash())) != 0 {
			t.Fatalf("HASHSU bits=%d refs=%d mismatch", bits, refs)
		}

		st = newCellSliceState()
		pushCellSliceCell(t, st, cl)
		if err := CDEPTH().Interpret(st); err != nil {
			t.Fatalf("CDEPTH bits=%d refs=%d failed: %v", bits, refs, err)
		}
		if got := popCellSliceInt(t, st); got != int64(cl.Depth()) {
			t.Fatalf("CDEPTH bits=%d refs=%d = %d, want %d", bits, refs, got, cl.Depth())
		}
	})
}

func hashDepthFuzzCell(t *testing.T, bits uint, refs int, fill byte) *cell.Cell {
	t.Helper()

	b := cell.BeginCell()
	if bits > 0 {
		buf := make([]byte, (bits+7)/8)
		for i := range buf {
			buf[i] = fill ^ byte(i*17)
		}
		b.MustStoreSlice(buf, bits)
	}
	for i := 0; i < refs; i++ {
		b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
	}
	return b.EndCell()
}
