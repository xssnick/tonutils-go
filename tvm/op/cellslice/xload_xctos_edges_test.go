package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestXCTOSAndXLOADStackTypeEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   cellSliceTransformOp
	}{
		{name: "XCTOS", op: XCTOS()},
		{name: "XLOAD", op: XLOAD()},
		{name: "XLOADQ", op: XLOADQ()},
	} {
		t.Run(tt.name+"StackUnderflow", func(t *testing.T) {
			assertCellSliceVMErrorCode(t, tt.op.Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)
		})

		t.Run(tt.name+"TypeCheck", func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceInt(t, st, 0)
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}
}

func TestXLOADQPostV5OrdinarySuccess(t *testing.T) {
	ordinary := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	st := newCellSliceState()
	pushCellSliceCell(t, st, ordinary)
	if err := XLOADQ().Interpret(st); err != nil {
		t.Fatalf("XLOADQ ordinary failed: %v", err)
	}
	if ok := popCellSliceBool(t, st); !ok {
		t.Fatal("XLOADQ ordinary flag = false, want true")
	}
	if got := popCellSliceCell(t, st); !sameCellHash(got, ordinary) {
		t.Fatal("XLOADQ ordinary should preserve ordinary cell")
	}
}
