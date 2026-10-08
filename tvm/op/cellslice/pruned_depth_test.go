package cellslice

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestENDXCPrunedDepthLimit(t *testing.T) {
	for _, depth := range []uint64{1024, 1025, 65535} {
		t.Run(fmt.Sprintf("depth%d", depth), func(t *testing.T) {
			builder := cell.BeginCell().MustStoreUInt(uint64(cell.PrunedCellType), 8).
				MustStoreUInt(1, 8).MustStoreSlice(make([]byte, 32), 256).MustStoreUInt(depth, 16)
			state := newCellSliceState()
			pushCellSliceBuilder(t, state, builder)
			pushCellSliceBool(t, state, true)
			err := ENDXC().Interpret(state)
			if depth > 1024 {
				assertCellSliceVMErrorCode(t, err, vmerr.CodeCellOverflow)
				return
			}
			if err != nil {
				t.Fatalf("ENDXC rejected depth 1024: %v", err)
			}
			if got := popCellSliceCell(t, state).Depth(0); uint64(got) != depth {
				t.Fatalf("stored depth = %d, want %d", got, depth)
			}
		})
	}
}
