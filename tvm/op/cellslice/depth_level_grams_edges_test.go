package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestDepthLevelAndStoreGramsEdges(t *testing.T) {
	t.Run("SDEPTHStackTypeAndValue", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, SDEPTH().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, SDEPTH().Interpret(st), vmerr.CodeTypeCheck)

		nested := cell.BeginCell().
			MustStoreRef(cell.BeginCell().
				MustStoreRef(cell.BeginCell().EndCell()).
				EndCell()).
			EndCell()
		st = newCellSliceState()
		pushCellSliceSlice(t, st, nested.MustBeginParse())
		if err := SDEPTH().Interpret(st); err != nil {
			t.Fatalf("SDEPTH nested failed: %v", err)
		}
		if got := popCellSliceInt(t, st); got != 2 {
			t.Fatalf("SDEPTH nested = %d, want 2", got)
		}
	})

	for _, tt := range []struct {
		name string
		op   cellSliceTransformOp
	}{
		{name: "CLEVEL", op: CLEVEL()},
		{name: "CLEVELMASK", op: CLEVELMASK()},
	} {
		t.Run(tt.name+"StackTypeAndVersion", func(t *testing.T) {
			if got := tt.op.(interface{ MinGlobalVersion() int }).MinGlobalVersion(); got != 6 {
				t.Fatalf("%s min version = %d, want 6", tt.name, got)
			}

			assertCellSliceVMErrorCode(t, tt.op.Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

			st := newCellSliceState()
			pushCellSliceInt(t, st, 0)
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}

	t.Run("CLEVELAndMaskPrunedCell", func(t *testing.T) {
		pruned := mustPrunedCell(t)

		st := newCellSliceState()
		pushCellSliceCell(t, st, pruned)
		if err := CLEVEL().Interpret(st); err != nil {
			t.Fatalf("CLEVEL pruned failed: %v", err)
		}
		if got := popCellSliceInt(t, st); got != 1 {
			t.Fatalf("CLEVEL pruned = %d, want 1", got)
		}

		st = newCellSliceState()
		pushCellSliceCell(t, st, pruned)
		if err := CLEVELMASK().Interpret(st); err != nil {
			t.Fatalf("CLEVELMASK pruned failed: %v", err)
		}
		if got := popCellSliceInt(t, st); got != 1 {
			t.Fatalf("CLEVELMASK pruned = %d, want 1", got)
		}
	})

	t.Run("STGRAMSStackTypeRangeAndShape", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, STGRAMS().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STGRAMS().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, STGRAMS().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, STGRAMS().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		if err := st.Stack.PushInt(new(big.Int).Lsh(big.NewInt(1), 120)); err != nil {
			t.Fatalf("push large coin value: %v", err)
		}
		if err := STGRAMS().Interpret(st); err == nil {
			t.Fatal("STGRAMS too-large coin value unexpectedly succeeded")
		}

		want := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 120), big.NewInt(1))
		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell().MustStoreUInt(0xA, 4))
		if err := st.Stack.PushInt(want); err != nil {
			t.Fatalf("push max coin value: %v", err)
		}
		if err := STGRAMS().Interpret(st); err != nil {
			t.Fatalf("STGRAMS max failed: %v", err)
		}
		gotBuilder := popCellSliceBuilder(t, st)
		gotSlice := gotBuilder.EndCell().MustBeginParse()
		if gotPrefix := gotSlice.MustLoadUInt(4); gotPrefix != 0xA {
			t.Fatalf("STGRAMS prefix = %x, want a", gotPrefix)
		}
		if got := gotSlice.MustLoadBigCoins(); got.Cmp(want) != 0 {
			t.Fatalf("STGRAMS coins = %s, want %s", got, want)
		}
		if gotSlice.BitsLeft() != 0 || gotSlice.RefsNum() != 0 {
			t.Fatalf("STGRAMS rest=(%d,%d), want empty", gotSlice.BitsLeft(), gotSlice.RefsNum())
		}
	})
}

func TestQuietSliceCheckTypeAndRangePriority(t *testing.T) {
	t.Run("SCHKBITREFSQStackAndTypes", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceInt(t, st, 8)
		assertCellSliceVMErrorCode(t, SCHKBITREFSQ().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceInt(t, st, 8)
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, SCHKBITREFSQ().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, SCHKBITREFSQ().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 8)
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, SCHKBITREFSQ().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceInt(t, st, 8)
		pushCellSliceInt(t, st, 5)
		assertCellSliceVMErrorCode(t, SCHKBITREFSQ().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceInt(t, st, 1024)
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, SCHKBITREFSQ().Interpret(st), vmerr.CodeRangeCheck)
	})
}

func FuzzTVMStoreGramsRoundTrip(f *testing.F) {
	for _, seed := range []uint64{0, 1, 0xF, 0x10, 0xFFFF, 1 << 32, ^uint64(0)} {
		f.Add(seed)
	}

	limit := new(big.Int).Lsh(big.NewInt(1), 120)
	f.Fuzz(func(t *testing.T, raw uint64) {
		coins := new(big.Int).SetUint64(raw)
		if raw&1 != 0 {
			coins.Lsh(coins, uint(raw%57))
		}
		coins.Mod(coins, limit)

		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		if err := st.Stack.PushInt(coins); err != nil {
			t.Fatalf("push coins: %v", err)
		}
		if err := STGRAMS().Interpret(st); err != nil {
			t.Fatalf("STGRAMS coins=%s failed: %v", coins, err)
		}

		gotBuilder := popCellSliceBuilder(t, st)
		got := gotBuilder.EndCell().MustBeginParse().MustLoadBigCoins()
		if got.Cmp(coins) != 0 {
			t.Fatalf("STGRAMS round trip = %s, want %s", got, coins)
		}
	})
}
