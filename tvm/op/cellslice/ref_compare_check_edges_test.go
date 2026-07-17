package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestLDREFRTOSSDEQAndSCHKREFSEdges(t *testing.T) {
	t.Run("LDREFRTOSStackTypeAndUnderflow", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, LDREFRTOS().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDREFRTOS().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice())
		if err := LDREFRTOS().Interpret(st); err == nil {
			t.Fatal("LDREFRTOS without refs unexpectedly succeeded")
		}
	})

	t.Run("SDEQStackTypeAndFalse", func(t *testing.T) {
		left := cell.BeginCell().MustStoreUInt(0b1010, 4).ToSlice()
		right := cell.BeginCell().MustStoreUInt(0b1011, 4).ToSlice()

		st := newCellSliceState()
		pushCellSliceSlice(t, st, left.Copy())
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, SDEQ().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceSlice(t, st, right.Copy())
		assertCellSliceVMErrorCode(t, SDEQ().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, left.Copy())
		pushCellSliceSlice(t, st, right.Copy())
		if err := SDEQ().Interpret(st); err != nil {
			t.Fatalf("SDEQ false failed: %v", err)
		}
		if got := popCellSliceBool(t, st); got {
			t.Fatal("SDEQ different bits = true, want false")
		}
	})

	t.Run("SCHKREFSAndQuietStackTypePriority", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			op   cellSliceTransformOp
		}{
			{name: "SCHKREFS", op: SCHKREFS()},
			{name: "SCHKREFSQ", op: SCHKREFSQ()},
		} {
			t.Run(tt.name+"ShortStack", func(t *testing.T) {
				st := newCellSliceState()
				pushCellSliceInt(t, st, 0)
				assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
			})

			t.Run(tt.name+"RefsType", func(t *testing.T) {
				st := newCellSliceState()
				pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 0, 1))
				pushCellSliceCell(t, st, cell.BeginCell().EndCell())
				assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
			})

			t.Run(tt.name+"SliceType", func(t *testing.T) {
				st := newCellSliceState()
				pushCellSliceCell(t, st, cell.BeginCell().EndCell())
				pushCellSliceInt(t, st, 0)
				assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
			})
		}
	})
}

func FuzzTVMSDEQBitOnlyRules(f *testing.F) {
	for _, seed := range []struct {
		leftBits  byte
		leftValue uint16
		leftRefs  byte
		rightBits byte
		rightVal  uint16
		rightRefs byte
	}{
		{leftBits: 4, leftValue: 0b1010, leftRefs: 1, rightBits: 4, rightVal: 0b1010},
		{leftBits: 4, leftValue: 0b1010, rightBits: 4, rightVal: 0b1011},
		{leftBits: 3, leftValue: 0b101, rightBits: 4, rightVal: 0b1010},
	} {
		f.Add(seed.leftBits, seed.leftValue, seed.leftRefs, seed.rightBits, seed.rightVal, seed.rightRefs)
	}

	f.Fuzz(func(t *testing.T, rawLeftBits byte, rawLeftValue uint16, rawLeftRefs byte, rawRightBits byte, rawRightValue uint16, rawRightRefs byte) {
		leftBits := uint(rawLeftBits % 17)
		rightBits := uint(rawRightBits % 17)
		leftValue := uint64(rawLeftValue) & bitMask(leftBits)
		rightValue := uint64(rawRightValue) & bitMask(rightBits)
		left := sdeqFuzzSlice(leftBits, leftValue, int(rawLeftRefs%5))
		right := sdeqFuzzSlice(rightBits, rightValue, int(rawRightRefs%5))

		st := newCellSliceState()
		pushCellSliceSlice(t, st, left.Copy())
		pushCellSliceSlice(t, st, right.Copy())
		if err := SDEQ().Interpret(st); err != nil {
			t.Fatalf("SDEQ failed: %v", err)
		}
		want := leftBits == rightBits && leftValue == rightValue
		if got := popCellSliceBool(t, st); got != want {
			t.Fatalf("SDEQ bits=(%d,%x)/(%d,%x) = %v, want %v", leftBits, leftValue, rightBits, rightValue, got, want)
		}
	})
}

func sdeqFuzzSlice(bits uint, value uint64, refs int) *cell.Slice {
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreUInt(value, bits)
	}
	for i := 0; i < refs; i++ {
		b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
	}
	return b.ToSlice()
}

func bitMask(bits uint) uint64 {
	if bits == 0 {
		return 0
	}
	return (uint64(1) << bits) - 1
}
