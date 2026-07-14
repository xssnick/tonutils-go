package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestSplitStackTypeRangeAndQuietEdges(t *testing.T) {
	src := splitEdgeSlice(6, 0b101100, 2)

	t.Run("StackAndTypePriority", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceInt(t, st, 0)
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, SPLIT().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceInt(t, st, 0)
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, SPLIT().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, SPLIT().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 0)
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, SPLIT().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("RangePriority", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceInt(t, st, 0)
		pushCellSliceInt(t, st, 5)
		assertCellSliceVMErrorCode(t, SPLIT().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceInt(t, st, 1024)
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, SPLIT().Interpret(st), vmerr.CodeRangeCheck)
	})

	t.Run("QuietSuccessAndFailureShapes", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceInt(t, st, 3)
		pushCellSliceInt(t, st, 1)
		if err := SPLITQ().Interpret(st); err != nil {
			t.Fatalf("SPLITQ success failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("SPLITQ success flag = false")
		}
		rest := popCellSliceSlice(t, st)
		first := popCellSliceSlice(t, st)
		if first.BitsLeft() != 3 || first.RefsNum() != 1 || rest.BitsLeft() != 3 || rest.RefsNum() != 1 {
			t.Fatalf("unexpected SPLITQ success shape first=(%d,%d) rest=(%d,%d)", first.BitsLeft(), first.RefsNum(), rest.BitsLeft(), rest.RefsNum())
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceInt(t, st, 7)
		pushCellSliceInt(t, st, 0)
		if err := SPLITQ().Interpret(st); err != nil {
			t.Fatalf("SPLITQ failure failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); ok {
			t.Fatal("SPLITQ failure flag = true")
		}
		if preserved := popCellSliceSlice(t, st); preserved.BitsLeft() != 6 || preserved.RefsNum() != 2 {
			t.Fatalf("SPLITQ failure preserved shape=(%d,%d), want (6,2)", preserved.BitsLeft(), preserved.RefsNum())
		}
	})
}

func FuzzTVMSplitRules(f *testing.F) {
	for _, seed := range []struct {
		haveBits uint16
		haveRefs byte
		takeBits uint16
		takeRefs byte
		quiet    bool
	}{
		{haveBits: 6, haveRefs: 2, takeBits: 3, takeRefs: 1},
		{haveBits: 6, haveRefs: 2, takeBits: 7, takeRefs: 0},
		{haveBits: 6, haveRefs: 2, takeBits: 3, takeRefs: 3, quiet: true},
		{haveBits: 1023, haveRefs: 4, takeBits: 1023, takeRefs: 4, quiet: true},
		{haveBits: 8, haveRefs: 0, takeBits: 1024, takeRefs: 0},
		{haveBits: 8, haveRefs: 0, takeBits: 4, takeRefs: 5},
	} {
		f.Add(seed.haveBits, seed.haveRefs, seed.takeBits, seed.takeRefs, seed.quiet)
	}

	f.Fuzz(func(t *testing.T, rawHaveBits uint16, rawHaveRefs byte, rawTakeBits uint16, rawTakeRefs byte, quiet bool) {
		haveBits := uint(rawHaveBits % 1024)
		haveRefs := int(rawHaveRefs % 5)
		takeBits := int64(rawTakeBits % 1030)
		takeRefs := int64(rawTakeRefs % 7)

		st := newCellSliceState()
		pushCellSliceSlice(t, st, splitEdgeSlice(haveBits, 0xAA, haveRefs))
		pushCellSliceInt(t, st, takeBits)
		pushCellSliceInt(t, st, takeRefs)

		var err error
		if quiet {
			err = SPLITQ().Interpret(st)
		} else {
			err = SPLIT().Interpret(st)
		}

		if takeRefs > 4 || takeBits > 1023 {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
			return
		}
		if uint(takeBits) > haveBits || int(takeRefs) > haveRefs {
			if quiet {
				if err != nil {
					t.Fatalf("quiet underflow failed: %v", err)
				}
				if ok := popCellSliceBool(t, st); ok {
					t.Fatal("quiet underflow flag = true")
				}
				if preserved := popCellSliceSlice(t, st); preserved.BitsLeft() != haveBits || preserved.RefsNum() != haveRefs {
					t.Fatalf("quiet underflow preserved=(%d,%d), want=(%d,%d)", preserved.BitsLeft(), preserved.RefsNum(), haveBits, haveRefs)
				}
				return
			}
			assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
			return
		}

		if err != nil {
			t.Fatalf("split failed: %v", err)
		}
		if quiet {
			if ok := popCellSliceBool(t, st); !ok {
				t.Fatal("quiet success flag = false")
			}
		}
		rest := popCellSliceSlice(t, st)
		first := popCellSliceSlice(t, st)
		if first.BitsLeft() != uint(takeBits) || first.RefsNum() != int(takeRefs) {
			t.Fatalf("first=(%d,%d), want=(%d,%d)", first.BitsLeft(), first.RefsNum(), takeBits, takeRefs)
		}
		if rest.BitsLeft() != haveBits-uint(takeBits) || rest.RefsNum() != haveRefs-int(takeRefs) {
			t.Fatalf("rest=(%d,%d), want=(%d,%d)", rest.BitsLeft(), rest.RefsNum(), haveBits-uint(takeBits), haveRefs-int(takeRefs))
		}
	})
}

func splitEdgeSlice(bits uint, fill uint64, refs int) *cell.Slice {
	b := cell.BeginCell()
	for i := uint(0); i < bits; i++ {
		shift := bits - 1 - i
		b.MustStoreUInt((fill>>shift)&1, 1)
	}
	for i := 0; i < refs; i++ {
		b.MustStoreRef(cell.BeginCell().EndCell())
	}
	return b.EndCell().MustBeginParse()
}
