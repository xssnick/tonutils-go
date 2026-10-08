package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestConstStoreDeserializeAndPanicEdges(t *testing.T) {
	ref := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	t.Run("STREFCONSTMissingReferences", func(t *testing.T) {
		op := STREFCONST(nil)
		assertCellSliceVMErrorCode(t, op.Deserialize(cell.BeginCell().
			MustStoreUInt(0xCF20>>1, 15).
			MustStoreUInt(0, 1).
			EndCell().MustBeginParse()), vmerr.CodeInvalidOpcode)

		op = STREFCONST(nil)
		assertCellSliceVMErrorCode(t, op.Deserialize(cell.BeginCell().
			MustStoreUInt(0xCF20>>1, 15).
			MustStoreUInt(1, 1).
			MustStoreRef(ref).
			EndCell().MustBeginParse()), vmerr.CodeInvalidOpcode)
	})

	t.Run("STREFCONSTShortInstruction", func(t *testing.T) {
		if err := STREFCONST(nil).Deserialize(cell.BeginCell().MustStoreUInt(0, 14).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short STREFCONST prefix unexpectedly decoded")
		}
		if err := STREFCONST(nil).Deserialize(cell.BeginCell().MustStoreUInt(0xCF20>>1, 15).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short STREFCONST count bit unexpectedly decoded")
		}
	})

	t.Run("STREFCONSTSerializePanicsOnInvalidCount", func(t *testing.T) {
		assertCellSlicePanic(t, func() {
			(&OpSTREFCONST{}).Serialize()
		})
		assertCellSlicePanic(t, func() {
			(&OpSTREFCONST{refsNum: 3}).Serialize()
		})
	})

	t.Run("STSLICECONSTShortInstruction", func(t *testing.T) {
		if err := STSLICECONST(nil).Deserialize(cell.BeginCell().MustStoreUInt(0, 8).EndCell().MustBeginParse()); err == nil {
			t.Fatal("short STSLICECONST prefix unexpectedly decoded")
		}
		assertCellSliceVMErrorCode(t, STSLICECONST(nil).Deserialize(cell.BeginCell().
			MustStoreUInt(0xCF80>>7, 9).
			EndCell().MustBeginParse()), vmerr.CodeInvalidOpcode)
		assertCellSliceVMErrorCode(t, STSLICECONST(nil).Deserialize(cell.BeginCell().
			MustStoreUInt(0xCF80>>7, 9).
			MustStoreUInt(1, 5).
			MustStoreUInt(0, 2).
			EndCell().MustBeginParse()), vmerr.CodeInvalidOpcode)
		assertCellSliceVMErrorCode(t, STSLICECONST(nil).Deserialize(cell.BeginCell().
			MustStoreUInt(0xCF80>>7, 9).
			MustStoreUInt(8, 5).
			MustStoreUInt(0, 2).
			EndCell().MustBeginParse()), vmerr.CodeInvalidOpcode)
	})

	t.Run("STSLICECONSTSerializePanicsOnInvalidPayload", func(t *testing.T) {
		assertCellSlicePanic(t, func() {
			STSLICECONST(cell.BeginCell().
				MustStoreRef(ref).
				MustStoreRef(ref).
				MustStoreRef(ref).
				MustStoreRef(ref).
				ToSlice()).Serialize()
		})
		assertCellSlicePanic(t, func() {
			STSLICECONST(cell.BeginCell().MustStoreSlice(make([]byte, 8), 58).ToSlice()).Serialize()
		})
	})
}

func TestConstStoreInterpretErrorEdges(t *testing.T) {
	ref := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	t.Run("STREFCONSTStackOverflowAndNilRef", func(t *testing.T) {
		st := newCellSliceState()
		assertCellSliceVMErrorCode(t, STREFCONST(ref).Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceCell(t, st, ref)
		assertCellSliceVMErrorCode(t, STREFCONST(ref).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, mustFullBuilder(t))
		assertCellSliceVMErrorCode(t, STREFCONST(ref).Interpret(st), vmerr.CodeCellOverflow)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STREFCONST(nil).Interpret(st), vmerr.CodeCellOverflow)
	})

	t.Run("STSLICECONSTNilAndOverflow", func(t *testing.T) {
		op := STSLICECONST(nil)
		if op.value.BitsLeft() != 0 || op.value.RefsNum() != 0 {
			t.Fatalf("nil STSLICECONST value is not empty: bits=%d refs=%d", op.value.BitsLeft(), op.value.RefsNum())
		}

		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell().MustStoreUInt(1, 1))
		if err := op.Interpret(st); err != nil {
			t.Fatalf("empty STSLICECONST failed: %v", err)
		}
		if got := popCellSliceBuilder(t, st); got.BitsUsed() != 1 || got.RefsUsed() != 0 {
			t.Fatalf("empty STSLICECONST changed builder shape: bits=%d refs=%d", got.BitsUsed(), got.RefsUsed())
		}

		st = newCellSliceState()
		assertCellSliceVMErrorCode(t, STSLICECONST(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()).Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceCell(t, st, ref)
		assertCellSliceVMErrorCode(t, STSLICECONST(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell().MustStoreSlice(make([]byte, 128), 1023))
		assertCellSliceVMErrorCode(t, STSLICECONST(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()).Interpret(st), vmerr.CodeCellOverflow)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, mustFullBuilder(t))
		assertCellSliceVMErrorCode(t, STSLICECONST(cell.BeginCell().MustStoreRef(ref).ToSlice()).Interpret(st), vmerr.CodeCellOverflow)

	})

	t.Run("ENDCSTStackTypeAndSourceErrors", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, ENDCST().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceCell(t, st, ref)
		assertCellSliceVMErrorCode(t, ENDCST().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, ref)
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, ENDCST().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceBuilder(t, st, builderWithDeepUncheckedRef(t))
		assertCellSliceVMErrorCode(t, ENDCST().Interpret(st), vmerr.CodeCellOverflow)

	})

	t.Run("RemainderOpsTypeErrors", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			op   cellSliceTransformOp
		}{
			{name: "BREMBITS", op: BREMBITS()},
			{name: "BREMREFS", op: BREMREFS()},
			{name: "BREMBITREFS", op: BREMBITREFS()},
		} {
			t.Run(tt.name, func(t *testing.T) {
				st := newCellSliceState()
				pushCellSliceCell(t, st, ref)
				assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
			})
		}
	})

	t.Run("RemainderOpsUnderflow", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			op   cellSliceTransformOp
		}{
			{name: "BREMBITS", op: BREMBITS()},
			{name: "BREMREFS", op: BREMREFS()},
			{name: "BREMBITREFS", op: BREMBITREFS()},
		} {
			t.Run(tt.name+"Underflow", func(t *testing.T) {
				assertCellSliceVMErrorCode(t, tt.op.Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)
			})
		}
	})
}

func TestConstStorePayloadNoPaddingBranch(t *testing.T) {
	payload := encodeConstStoreSlicePayload(cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice(), 8).EndCell().MustBeginParse()
	if payload.BitsLeft() != 8 || payload.MustLoadUInt(8) != 0xAB {
		t.Fatal("payload should be unchanged when totalBits equals actual bits")
	}
}

func builderWithDeepUncheckedRef(t *testing.T) *cell.Builder {
	t.Helper()
	builder := cell.BeginCell()
	if err := builder.StoreRefUncheckedDepth(depthLimitCell()); err != nil {
		t.Fatalf("store unchecked deep ref: %v", err)
	}
	return builder
}

func assertCellSlicePanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}
