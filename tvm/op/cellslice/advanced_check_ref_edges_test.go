package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestAdvancedSliceCheckErrorAndStackEdges(t *testing.T) {
	t.Run("NonQuietFailuresConsumeInputs", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			op   cellSliceTransformOp
			args []int64
		}{
			{name: "SCHKBITS", op: SCHKBITS(), args: []int64{9}},
			{name: "SCHKREFS", op: SCHKREFS(), args: []int64{2}},
			{name: "SCHKBITREFSBits", op: SCHKBITREFS(), args: []int64{9, 1}},
			{name: "SCHKBITREFSRefs", op: SCHKBITREFS(), args: []int64{8, 2}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				st := newCellSliceState()
				pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
				for _, arg := range tt.args {
					pushCellSliceInt(t, st, arg)
				}
				assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeCellUnderflow)
				if st.Stack.Len() != 0 {
					t.Fatalf("%s stack depth = %d, want 0", tt.name, st.Stack.Len())
				}
			})
		}
	})

	t.Run("QuietFalseShape", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			op   cellSliceTransformOp
			args []int64
		}{
			{name: "SCHKBITSQ", op: SCHKBITSQ(), args: []int64{9}},
			{name: "SCHKREFSQ", op: SCHKREFSQ(), args: []int64{2}},
			{name: "SCHKBITREFSQBits", op: SCHKBITREFSQ(), args: []int64{9, 1}},
			{name: "SCHKBITREFSQRefs", op: SCHKBITREFSQ(), args: []int64{8, 2}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				st := newCellSliceState()
				pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
				for _, arg := range tt.args {
					pushCellSliceInt(t, st, arg)
				}
				if err := tt.op.Interpret(st); err != nil {
					t.Fatalf("%s failed: %v", tt.name, err)
				}
				if ok := popCellSliceBool(t, st); ok {
					t.Fatalf("%s quiet flag = true, want false", tt.name)
				}
				if st.Stack.Len() != 0 {
					t.Fatalf("%s stack depth = %d, want 0", tt.name, st.Stack.Len())
				}
			})
		}
	})

	t.Run("RangeAndTypePriority", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, SCHKBITS().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, SCHKBITSQ().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceInt(t, st, 8)
		assertCellSliceVMErrorCode(t, SCHKBITREFS().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, SCHKBITS().Interpret(st), vmerr.CodeRangeCheck)
		if st.Stack.Len() != 1 {
			t.Fatalf("SCHKBITS range stack depth = %d, want 1", st.Stack.Len())
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, SCHKREFS().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceInt(t, st, 8)
		pushCellSliceInt(t, st, 5)
		assertCellSliceVMErrorCode(t, SCHKBITREFS().Interpret(st), vmerr.CodeRangeCheck)
		if st.Stack.Len() != 2 {
			t.Fatalf("SCHKBITREFS refs range stack depth = %d, want 2", st.Stack.Len())
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceInt(t, st, 1024)
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, SCHKBITREFS().Interpret(st), vmerr.CodeRangeCheck)
		if st.Stack.Len() != 1 {
			t.Fatalf("SCHKBITREFS bits range stack depth = %d, want 1", st.Stack.Len())
		}

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, SCHKBITSQ().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 8, 1))
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, SCHKREFSQ().Interpret(st), vmerr.CodeTypeCheck)
	})
}

func TestAdvancedRefPreloadEdges(t *testing.T) {
	ref0 := cell.BeginCell().MustStoreUInt(0xA0, 8).EndCell()
	ref1 := cell.BeginCell().MustStoreUInt(0xB1, 8).EndCell()
	ref2 := cell.BeginCell().MustStoreUInt(0xC2, 8).EndCell()
	ref3 := cell.BeginCell().MustStoreUInt(0xD3, 8).EndCell()
	src := cell.BeginCell().MustStoreRef(ref0).MustStoreRef(ref1).MustStoreRef(ref2).MustStoreRef(ref3).ToSlice()

	t.Run("PLDREFVARSuccessAndErrors", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceInt(t, st, 3)
		if err := PLDREFVAR().Interpret(st); err != nil {
			t.Fatalf("PLDREFVAR idx 3 failed: %v", err)
		}
		if !sameCellHash(popCellSliceCell(t, st), ref3) {
			t.Fatal("PLDREFVAR idx 3 ref mismatch")
		}

		st = newCellSliceState()
		pushCellSliceInt(t, st, 4)
		assertCellSliceVMErrorCode(t, PLDREFVAR().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceInt(t, st, 4)
		assertCellSliceVMErrorCode(t, PLDREFVAR().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, PLDREFVAR().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, PLDREFVAR().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreRef(ref0).ToSlice())
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, PLDREFVAR().Interpret(st), vmerr.CodeCellUnderflow)
	})

	t.Run("PLDREFIDXDeserializeSuccessAndErrors", func(t *testing.T) {
		op := PLDREFIDX(0)
		if err := op.Deserialize(PLDREFIDX(3).Serialize().EndCell().MustBeginParse()); err != nil {
			t.Fatalf("PLDREFIDX deserialize: %v", err)
		}
		if got := op.SerializeText(); got != "PLDREFIDX 3" {
			t.Fatalf("PLDREFIDX text = %q, want PLDREFIDX 3", got)
		}
		if got := op.InstructionBits(); got != 16 {
			t.Fatalf("PLDREFIDX instruction bits = %d, want 16", got)
		}

		st := newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		if err := op.Interpret(st); err != nil {
			t.Fatalf("PLDREFIDX idx 3 failed: %v", err)
		}
		if !sameCellHash(popCellSliceCell(t, st), ref3) {
			t.Fatal("PLDREFIDX idx 3 ref mismatch")
		}

		if err := PLDREFIDX(0).Deserialize(cell.BeginCell().MustStoreUInt(0xD74C>>2, 14).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short PLDREFIDX suffix unexpectedly decoded")
		}

		st = newCellSliceState()
		assertCellSliceVMErrorCode(t, PLDREFIDX(0).Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, PLDREFIDX(0).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cellSliceSingleRef(ref0))
		assertCellSliceVMErrorCode(t, PLDREFIDX(1).Interpret(st), vmerr.CodeCellUnderflow)
	})
}

func TestAdvancedSliceCountTypeErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   cellSliceTransformOp
	}{
		{name: "SBITS", op: SBITS()},
		{name: "SREFS", op: SREFS()},
		{name: "SBITREFS", op: SBITREFS()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceCell(t, st, cell.BeginCell().EndCell())
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}

	for _, tt := range []struct {
		name string
		op   cellSliceTransformOp
	}{
		{name: "SBITS", op: SBITS()},
		{name: "SREFS", op: SREFS()},
		{name: "SBITREFS", op: SBITREFS()},
	} {
		t.Run(tt.name+"Underflow", func(t *testing.T) {
			assertCellSliceVMErrorCode(t, tt.op.Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)
		})
	}

	t.Run("SBITREFSStackOverflowLeavesBits", func(t *testing.T) {
		st := newCellSliceState()
		fillAdvancedBuilderStack(t, st, 1)
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, 7, 1))
		assertCellSliceVMErrorCode(t, SBITREFS().Interpret(st), vmerr.CodeStackOverflow)

		if got := popCellSliceInt(t, st); got != 7 {
			t.Fatalf("partial SBITREFS bits = %d, want 7", got)
		}
	})
}

func FuzzTVMSliceCheckAndRefRules(f *testing.F) {
	for _, seed := range []struct {
		op       byte
		haveBits uint16
		haveRefs byte
		wantBits uint16
		wantRefs byte
	}{
		{op: 0, haveBits: 8, haveRefs: 1, wantBits: 8},
		{op: 0, haveBits: 8, haveRefs: 1, wantBits: 9},
		{op: 1, haveBits: 8, haveRefs: 1, wantRefs: 1},
		{op: 1, haveBits: 8, haveRefs: 1, wantRefs: 2},
		{op: 2, haveBits: 8, haveRefs: 1, wantBits: 8, wantRefs: 1},
		{op: 2, haveBits: 8, haveRefs: 1, wantBits: 9, wantRefs: 1},
		{op: 2, haveBits: 8, haveRefs: 1, wantBits: 8, wantRefs: 2},
		{op: 3, haveBits: 8, haveRefs: 4, wantRefs: 3},
		{op: 3, haveBits: 8, haveRefs: 2, wantRefs: 3},
		{op: 3, haveBits: 8, haveRefs: 4, wantRefs: 4},
	} {
		f.Add(seed.op, seed.haveBits, seed.haveRefs, seed.wantBits, seed.wantRefs)
	}

	f.Fuzz(func(t *testing.T, rawOp byte, rawHaveBits uint16, rawHaveRefs byte, rawWantBits uint16, rawWantRefs byte) {
		haveBits := uint(rawHaveBits % 1024)
		haveRefs := int(rawHaveRefs % 5)
		wantBits := int64(rawWantBits % 1030)
		wantRefs := int64(rawWantRefs % 6)

		st := newCellSliceState()
		pushCellSliceSlice(t, st, checkRefEdgeSlice(t, haveBits, haveRefs))

		switch rawOp % 4 {
		case 0:
			pushCellSliceInt(t, st, wantBits)
			err := SCHKBITSQ().Interpret(st)
			checkSliceCheckFuzzResult(t, st, err, wantBits <= int64(haveBits), wantBits > 1023)
		case 1:
			pushCellSliceInt(t, st, wantRefs)
			err := SCHKREFSQ().Interpret(st)
			checkSliceCheckFuzzResult(t, st, err, wantRefs <= int64(haveRefs), wantRefs > 1023)
		case 2:
			pushCellSliceInt(t, st, wantBits)
			pushCellSliceInt(t, st, wantRefs)
			err := SCHKBITREFSQ().Interpret(st)
			if wantRefs > 4 || wantBits > 1023 {
				assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
				return
			}
			checkSliceCheckFuzzResult(t, st, err, wantBits <= int64(haveBits) && wantRefs <= int64(haveRefs), false)
		default:
			pushCellSliceInt(t, st, wantRefs)
			err := PLDREFVAR().Interpret(st)
			if wantRefs > 3 {
				assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
				return
			}
			if wantRefs >= int64(haveRefs) {
				assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
				return
			}
			if err != nil {
				t.Fatalf("PLDREFVAR haveRefs=%d idx=%d failed: %v", haveRefs, wantRefs, err)
			}
			_ = popCellSliceCell(t, st)
		}
	})
}

func checkSliceCheckFuzzResult(t *testing.T, st *vm.State, err error, want bool, rangeCheck bool) {
	t.Helper()
	if rangeCheck {
		assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
		return
	}
	if err != nil {
		t.Fatalf("quiet slice check failed: %v", err)
	}
	if got := popCellSliceBool(t, st); got != want {
		t.Fatalf("quiet slice check = %v, want %v", got, want)
	}
}

func checkRefEdgeSlice(t *testing.T, bits uint, refs int) *cell.Slice {
	t.Helper()
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreSlice(make([]byte, (bits+7)/8), bits)
	}
	for i := 0; i < refs; i++ {
		b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
	}
	return b.ToSlice()
}

func cellSliceSingleRef(ref *cell.Cell) *cell.Slice {
	return cell.BeginCell().MustStoreRef(ref).ToSlice()
}
