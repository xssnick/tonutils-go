package cellslice

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestAdditionalCellSliceErrorAndSpecialBranches(t *testing.T) {
	t.Run("ENDCSTOverflow", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, mustFullBuilder(t))
		pushCellSliceBuilder(t, st, cell.BeginCell())
		if err := ENDCST().Interpret(st); err == nil {
			t.Fatal("ENDCST should fail when destination builder has no free refs")
		}
	})

	t.Run("SplitVariants", func(t *testing.T) {
		src := cell.BeginCell().
			MustStoreUInt(0b101011, 6).
			MustStoreRef(cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()).
			EndCell().BeginParse()

		st := newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceInt(t, st, 3)
		pushCellSliceInt(t, st, 1)
		if err := SPLIT().Interpret(st); err != nil {
			t.Fatalf("SPLIT failed: %v", err)
		}
		rest := popCellSliceSlice(t, st)
		first := popCellSliceSlice(t, st)
		if first.MustLoadUInt(3) != 0b101 || first.RefsNum() != 1 || rest.MustLoadUInt(3) != 0b011 || rest.RefsNum() != 0 {
			t.Fatalf("unexpected SPLIT result")
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceInt(t, st, 10)
		pushCellSliceInt(t, st, 0)
		if err := SPLIT().Interpret(st); err == nil {
			t.Fatal("SPLIT should fail on underflow")
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, src.Copy())
		pushCellSliceInt(t, st, 10)
		pushCellSliceInt(t, st, 0)
		if err := SPLITQ().Interpret(st); err != nil {
			t.Fatalf("SPLITQ should fail quietly: %v", err)
		}
		ok := popCellSliceBool(t, st)
		preserved := popCellSliceSlice(t, st)
		if ok || preserved.BitsLeft() != src.BitsLeft() || preserved.RefsNum() != src.RefsNum() {
			t.Fatalf("unexpected SPLITQ quiet result")
		}
	})

	t.Run("XCTOSMarksSpecialCells", func(t *testing.T) {
		st := newCellSliceState()
		special := mustPrunedCell(t)
		pushCellSliceCell(t, st, special)
		if err := XCTOS().Interpret(st); err != nil {
			t.Fatalf("XCTOS failed: %v", err)
		}
		isSpecial := popCellSliceBool(t, st)
		sl := popCellSliceSlice(t, st)
		if !isSpecial || sl == nil {
			t.Fatalf("expected special cell slice and true flag")
		}
	})

	t.Run("VirtualizedLoadPathsReturnExit14", func(t *testing.T) {
		body, pruned := mustVirtualizedProofBodyAndPrunedRef(t)

		st := newCellSliceState()
		pushCellSliceCell(t, st, pruned)
		if err := CTOS().Interpret(st); err == nil {
			t.Fatal("expected CTOS on virtualized pruned cell to fail")
		} else {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeVirtualization)
		}

		st = newCellSliceState()
		pushCellSliceCell(t, st, pruned)
		if err := XCTOS().Interpret(st); err == nil {
			t.Fatal("expected XCTOS on virtualized pruned cell to fail")
		} else {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeVirtualization)
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, body.BeginParse())
		if err := LDREFRTOS().Interpret(st); err == nil {
			t.Fatal("expected LDREFRTOS to fail when loading a virtualized pruned child")
		} else {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeVirtualization)
		}

		st = newCellSliceState()
		pushCellSliceCell(t, st, pruned)
		if err := XLOADQ().Interpret(st); err != nil {
			t.Fatalf("XLOADQ on virtualized pruned cell should fail quietly: %v", err)
		}
		if popCellSliceBool(t, st) {
			t.Fatal("expected XLOADQ failure flag on virtualized pruned cell")
		}

		st = newCellSliceState()
		pushCellSliceCell(t, st, pruned)
		if err := XLOAD().Interpret(st); err == nil {
			t.Fatal("expected XLOAD on virtualized pruned cell to fail")
		} else {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeCellUnderflow)
		}
	})

	t.Run("StoreOpsOverflow", func(t *testing.T) {
		ref := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

		st := newCellSliceState()
		pushCellSliceCell(t, st, ref)
		pushCellSliceBuilder(t, st, mustFullBuilder(t))
		if err := STREF().Interpret(st); err == nil {
			t.Fatal("STREF should fail on ref overflow")
		}

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell().MustStoreUInt(0xCD, 8))
		pushCellSliceBuilder(t, st, cell.BeginCell().MustStoreSlice(make([]byte, 127), 1016))
		if err := STB().Interpret(st); err == nil {
			t.Fatal("STB should fail on bit overflow")
		}

		st = newCellSliceState()
		pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xEF, 8).MustStoreRef(ref).EndCell().BeginParse())
		pushCellSliceBuilder(t, st, mustFullBuilder(t))
		if err := STSLICE().Interpret(st); err == nil {
			t.Fatal("STSLICE should fail when refs overflow")
		}

		st = newCellSliceState()
		pushCellSliceInt(t, st, 1)
		pushCellSliceBuilder(t, st, cell.BeginCell().MustStoreSlice(make([]byte, 127), 1016))
		if err := STGRAMS().Interpret(st); err == nil {
			t.Fatal("STGRAMS should fail on bit overflow")
		}
	})

	t.Run("CHASHIAndCDEPTHIRoundTrip", func(t *testing.T) {
		ref := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		cl := cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(ref).EndCell()

		hashOp := CHASHI(1)
		decodedHash := CHASHI(0)
		if err := decodedHash.Deserialize(hashOp.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("CHASHI deserialize failed: %v", err)
		}
		if got := decodedHash.SerializeText(); got != "CHASHI 1" {
			t.Fatalf("unexpected CHASHI text: %q", got)
		}

		st := newCellSliceState()
		pushCellSliceCell(t, st, cl)
		if err := decodedHash.Interpret(st); err != nil {
			t.Fatalf("CHASHI interpret failed: %v", err)
		}
		hash, err := st.Stack.PopInt()
		if err != nil {
			t.Fatalf("pop CHASHI hash: %v", err)
		}
		if hash.Sign() == 0 {
			t.Fatal("CHASHI should push a non-zero hash fragment")
		}

		depthOp := CDEPTHI(1)
		decodedDepth := CDEPTHI(0)
		if err := decodedDepth.Deserialize(depthOp.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("CDEPTHI deserialize failed: %v", err)
		}
		if got := decodedDepth.SerializeText(); got != "CDEPTHI 1" {
			t.Fatalf("unexpected CDEPTHI text: %q", got)
		}

		st = newCellSliceState()
		pushCellSliceCell(t, st, cl)
		if err := decodedDepth.Interpret(st); err != nil {
			t.Fatalf("CDEPTHI interpret failed: %v", err)
		}
		if got := popCellSliceInt(t, st); got != int64(cl.Depth(1)) {
			t.Fatalf("unexpected CDEPTHI depth: %d", got)
		}

	})

	t.Run("CHASHIXMatchesStaticVariant", func(t *testing.T) {
		cl := cell.BeginCell().MustStoreUInt(0xF0, 8).EndCell()

		staticState := newCellSliceState()
		pushCellSliceCell(t, staticState, cl)
		if err := CHASHI(0).Interpret(staticState); err != nil {
			t.Fatalf("CHASHI failed: %v", err)
		}
		staticHash, err := staticState.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop CHASHI hash: %v", err)
		}

		dynState := newCellSliceState()
		pushCellSliceCell(t, dynState, cl)
		pushCellSliceInt(t, dynState, 0)
		if err := CHASHIX().Interpret(dynState); err != nil {
			t.Fatalf("CHASHIX failed: %v", err)
		}
		dynHash, err := dynState.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop CHASHIX hash: %v", err)
		}
		if !bytes.Equal(staticHash.Bytes(), dynHash.Bytes()) {
			t.Fatalf("static/dynamic hash mismatch")
		}
	})
}
