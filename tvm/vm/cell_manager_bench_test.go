package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

var benchmarkCellManagerBits uint

func BenchmarkCellManagerLoadSet(b *testing.B) {
	var keys [16]cell.Hash
	for i := range keys {
		keys[i][0] = byte(i + 1)
	}

	for _, size := range []int{1, 4, 8, 16} {
		b.Run(loadSetBenchmarkName(size), func(b *testing.B) {
			state := State{Gas: GasWithLimit(GasInfinite)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var manager CellManager
				manager.Init(&state)
				for j := 0; j < size; j++ {
					if err := manager.RegisterCellLoadKey(keys[j]); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func loadSetBenchmarkName(size int) string {
	switch size {
	case 1:
		return "unique_1"
	case 4:
		return "unique_4"
	case 8:
		return "unique_8"
	default:
		return "unique_16"
	}
}

func BenchmarkCellManagerLoadRef(b *testing.B) {
	child := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	root := cell.BeginCell().MustStoreRef(child).EndCell()
	state := State{Gas: GasWithLimit(GasInfinite)}
	state.Cells.Init(&state)
	source, err := state.Cells.BeginParse(root)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input := *source
		loaded, err := state.Cells.LoadRef(&input)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkCellManagerBits = loaded.BitsLeft()
	}
}
