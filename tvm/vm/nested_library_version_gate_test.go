package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func mustNestedLibraryCell(t *testing.T, publishedHash []byte) *cell.Cell {
	t.Helper()

	lib, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(publishedHash, 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("build library cell: %v", err)
	}
	return lib
}

// FuzzCellManagerNestedLibraryVersionGate pins the historical gate covering the
// nested-library restriction in vm/cell_manager.go: before global version 5 a
// library cell may resolve to another library cell, and from version 5 on the
// chain is rejected with a cell-underflow error, matching the reference
// load_cell_slice_impl.
func FuzzCellManagerNestedLibraryVersionGate(f *testing.F) {
	for version := uint8(0); version <= uint8(MaxSupportedGlobalVersion); version++ {
		f.Add(version, uint8(0))
		f.Add(version, uint8(1))
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawSeed uint8) {
		version := int(rawVersion % uint8(MaxSupportedGlobalVersion+1))

		target := cell.BeginCell().MustStoreUInt(uint64(rawSeed), 8).EndCell()
		inner := mustNestedLibraryCell(t, target.Hash())
		outer := mustNestedLibraryCell(t, inner.Hash())

		st := NewExecutionState(version, GasWithLimit(1_000_000), nil, tuple.Tuple{}, NewStack())
		st.InitForExecution()
		st.SetLibraries(makeLibraryRoot(t, target), makeLibraryRoot(t, inner))

		_, err := st.Cells.BeginParse(outer)
		if version < 5 {
			if err != nil {
				t.Fatalf("v%d nested library chain must resolve, got %v", version, err)
			}
			return
		}
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeCellUnderflow {
			t.Fatalf("v%d nested library chain must be rejected with cell underflow, got %v", version, err)
		}
	})
}
