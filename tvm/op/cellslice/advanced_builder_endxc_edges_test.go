package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestAdvancedBuilderENDXCEdges(t *testing.T) {
	t.Run("StackAndTypeErrors", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, ENDXC().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, ENDXC().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceBool(t, st, false)
		assertCellSliceVMErrorCode(t, ENDXC().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("SpecialLibrarySuccess", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, librarySpecialBuilder())
		pushCellSliceBool(t, st, true)
		if err := ENDXC().Interpret(st); err != nil {
			t.Fatalf("ENDXC library special failed: %v", err)
		}
		cl := popCellSliceCell(t, st)
		if !cl.IsSpecial() || cl.GetType() != cell.LibraryCellType {
			t.Fatalf("unexpected ENDXC special cell: special=%v type=%v", cl.IsSpecial(), cl.GetType())
		}
	})

	t.Run("InvalidSpecialMapsToCellOverflow", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceBool(t, st, true)
		assertCellSliceVMErrorCode(t, ENDXC().Interpret(st), vmerr.CodeCellOverflow)
	})

	t.Run("EndBuilderCellMapsDepthErrorToCellOverflow", func(t *testing.T) {
		builder := cell.BeginCell()
		if err := builder.StoreRefUncheckedDepth(depthLimitCell()); err != nil {
			t.Fatalf("store unchecked deep ref: %v", err)
		}
		_, err := endBuilderCell(builder)
		assertCellSliceVMErrorCode(t, err, vmerr.CodeCellOverflow)
	})

	t.Run("EndBuilderCellPassesThroughVMError", func(t *testing.T) {
		const want int64 = vmerr.CodeOutOfGas
		builder := cell.BeginCell().SetTrace(cell.NewTrace(cell.TraceHooks{
			PendingError: func() error {
				return vmerr.Error(want)
			},
		}))
		_, err := endBuilderCell(builder)
		assertCellSliceVMErrorCode(t, err, want)
	})
}

func librarySpecialBuilder() *cell.Builder {
	return cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(make([]byte, 32), 256)
}

func depthLimitCell() *cell.Cell {
	cl := cell.BeginCell().EndCell()
	for i := 0; i < 1024; i++ {
		cl = cell.BeginCell().MustStoreRef(cl).EndCell()
	}
	return cl
}
