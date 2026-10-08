package funcs

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

var benchmarkStorageStatCells uint64

func storageStatBenchmarkTree(depth int) *cell.Cell {
	nextID := uint64(1)
	var build func(int) *cell.Cell
	build = func(left int) *cell.Cell {
		builder := cell.BeginCell().MustStoreUInt(nextID, 32)
		nextID++
		if left > 0 {
			for i := 0; i < 4; i++ {
				builder.MustStoreRef(build(left - 1))
			}
		}
		return builder.EndCell()
	}
	return build(depth)
}

func BenchmarkStorageStatTraversal(b *testing.B) {
	root := storageStatBenchmarkTree(3) // 85 unique cells.

	b.Run("unmetered", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			stat := newStorageStat(1<<63-1, nil)
			ok, err := stat.addCell(root)
			if err != nil || !ok {
				b.Fatalf("traversal failed: ok=%v err=%v", ok, err)
			}
			benchmarkStorageStatCells = stat.cells
		}
	})

	b.Run("metered", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			state := vm.State{Gas: vm.GasWithLimit(vm.GasInfinite)}
			state.InitForExecution()
			stat := newStorageStat(1<<63-1, &state)
			ok, err := stat.addCell(root)
			if err != nil || !ok {
				b.Fatalf("traversal failed: ok=%v err=%v", ok, err)
			}
			benchmarkStorageStatCells = stat.cells
		}
	})
}
