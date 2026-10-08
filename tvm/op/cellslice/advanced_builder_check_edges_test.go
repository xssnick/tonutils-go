package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type builderCheckOp interface {
	Interpret(*vm.State) error
}

func TestAdvancedBuilderCheckEdges(t *testing.T) {
	t.Run("BCHKBITSIMMDeserializeStackAndType", func(t *testing.T) {
		if err := BCHKBITSIMM(1, false).Deserialize(cell.BeginCell().MustStoreUInt(0xCF38, 16).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short BCHKBITS suffix unexpectedly decoded")
		}
		if err := BCHKBITSIMM(1, true).Deserialize(cell.BeginCell().MustStoreUInt(0xCF3C, 16).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short BCHKBITSQ suffix unexpectedly decoded")
		}

		assertCellSliceVMErrorCode(t, BCHKBITSIMM(8, false).Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)
		assertCellSliceVMErrorCode(t, BCHKBITSIMM(8, true).Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, BCHKBITSIMM(8, false).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, BCHKBITSIMM(8, true).Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("DynamicCheckStackTypeAndRangePriority", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			op   builderCheckOp
		}{
			{name: "BCHKBITS", op: BCHKBITS()},
			{name: "BCHKREFS", op: BCHKREFS()},
			{name: "BCHKBITREFS", op: BCHKBITREFS()},
			{name: "BCHKBITSQ", op: BCHKBITSQ()},
			{name: "BCHKREFSQ", op: BCHKREFSQ()},
			{name: "BCHKBITREFSQ", op: BCHKBITREFSQ()},
		} {
			t.Run(tt.name+"Underflow", func(t *testing.T) {
				assertCellSliceVMErrorCode(t, tt.op.Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)
			})
		}

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, BCHKBITS().Interpret(st), vmerr.CodeTypeCheck)
		if st.Stack.Len() != 0 {
			t.Fatalf("BCHKBITS builder typecheck stack depth = %d, want 0", st.Stack.Len())
		}

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, BCHKREFS().Interpret(st), vmerr.CodeTypeCheck)
		if st.Stack.Len() != 1 {
			t.Fatalf("BCHKREFS refs typecheck stack depth = %d, want 1", st.Stack.Len())
		}

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 1)
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, BCHKBITREFS().Interpret(st), vmerr.CodeTypeCheck)
		if st.Stack.Len() != 0 {
			t.Fatalf("BCHKBITREFS builder typecheck stack depth = %d, want 0", st.Stack.Len())
		}

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, BCHKBITS().Interpret(st), vmerr.CodeRangeCheck)
		if st.Stack.Len() != 1 {
			t.Fatalf("BCHKBITS range stack depth = %d, want 1", st.Stack.Len())
		}

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceInt(t, st, 8)
		assertCellSliceVMErrorCode(t, BCHKREFS().Interpret(st), vmerr.CodeRangeCheck)
		if st.Stack.Len() != 1 {
			t.Fatalf("BCHKREFS range stack depth = %d, want 1", st.Stack.Len())
		}

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceInt(t, st, 1024)
		pushCellSliceInt(t, st, 8)
		assertCellSliceVMErrorCode(t, BCHKBITREFS().Interpret(st), vmerr.CodeRangeCheck)
		if st.Stack.Len() != 2 {
			t.Fatalf("BCHKBITREFS refs range stack depth = %d, want 2", st.Stack.Len())
		}

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceInt(t, st, 1024)
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, BCHKBITREFS().Interpret(st), vmerr.CodeRangeCheck)
		if st.Stack.Len() != 1 {
			t.Fatalf("BCHKBITREFS bits range stack depth = %d, want 1", st.Stack.Len())
		}
	})

	t.Run("QuietFalse", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, reverseStoreEdgeBuilder(1023, 0))
		pushCellSliceInt(t, st, 1)
		if err := BCHKBITSQ().Interpret(st); err != nil {
			t.Fatalf("BCHKBITSQ failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); ok {
			t.Fatal("BCHKBITSQ flag = true, want false")
		}
	})
}

func FuzzTVMBuilderCheckRules(f *testing.F) {
	for _, seed := range []struct {
		op       byte
		haveBits uint16
		haveRefs byte
		wantBits uint16
		wantRefs byte
	}{
		{op: 0, haveBits: 1022, wantBits: 1},
		{op: 0, haveBits: 1023, wantBits: 1},
		{op: 1, haveRefs: 3, wantRefs: 1},
		{op: 1, haveRefs: 4, wantRefs: 1},
		{op: 2, haveBits: 1000, haveRefs: 3, wantBits: 23, wantRefs: 1},
		{op: 2, haveBits: 1000, haveRefs: 4, wantBits: 23, wantRefs: 1},
		{op: 2, haveBits: 8, wantBits: 1024, wantRefs: 0},
		{op: 2, haveBits: 8, wantBits: 1, wantRefs: 8},
		{op: 3, haveBits: 800, wantBits: 223},
		{op: 3, haveBits: 900, wantBits: 223},
	} {
		f.Add(seed.op, seed.haveBits, seed.haveRefs, seed.wantBits, seed.wantRefs)
	}

	f.Fuzz(func(t *testing.T, rawOp byte, rawHaveBits uint16, rawHaveRefs byte, rawWantBits uint16, rawWantRefs byte) {
		haveBits := uint(rawHaveBits % 1024)
		haveRefs := int(rawHaveRefs % 5)
		wantBits := int64(rawWantBits % 1030)
		wantRefs := int64(rawWantRefs % 10)

		st := newCellSliceState()
		pushCellSliceBuilder(t, st, reverseStoreEdgeBuilder(haveBits, haveRefs))

		switch rawOp % 4 {
		case 0:
			pushCellSliceInt(t, st, wantBits)
			err := BCHKBITSQ().Interpret(st)
			checkBuilderCheckFuzzResult(t, st, err, wantBits > 1023, haveBits+uint(wantBits) <= 1023)
		case 1:
			pushCellSliceInt(t, st, wantRefs)
			err := BCHKREFSQ().Interpret(st)
			checkBuilderCheckFuzzResult(t, st, err, wantRefs > 7, haveRefs+int(wantRefs) <= 4)
		case 2:
			pushCellSliceInt(t, st, wantBits)
			pushCellSliceInt(t, st, wantRefs)
			err := BCHKBITREFSQ().Interpret(st)
			if wantRefs > 7 || wantBits > 1023 {
				assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
				return
			}
			checkBuilderCheckFuzzResult(t, st, err, false, haveBits+uint(wantBits) <= 1023 && haveRefs+int(wantRefs) <= 4)
		case 3:
			immBits := uint(rawWantBits%256) + 1
			err := BCHKBITSIMM(immBits, true).Interpret(st)
			checkBuilderCheckFuzzResult(t, st, err, false, haveBits+immBits <= 1023)
		}
	})
}

func checkBuilderCheckFuzzResult(t *testing.T, st *vm.State, err error, wantRange bool, wantOK bool) {
	t.Helper()

	if wantRange {
		assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
		return
	}
	if err != nil {
		t.Fatalf("builder check failed: %v", err)
	}
	if ok := popCellSliceBool(t, st); ok != wantOK {
		t.Fatalf("builder check flag = %v, want %v", ok, wantOK)
	}
}
