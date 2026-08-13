package cell

import "testing"

// benchSeenSetShape mirrors what a shard collation actually puts through the
// dedup memo: tens of thousands of distinct cells fed once each, which is the
// regime where the table spends its time growing rather than probing. The
// smaller sizes stand in for the proof-side set, which the same type serves at
// a couple of orders of magnitude less.
var benchSeenSetShape = []struct {
	name  string
	cells int
}{
	{name: "proof-ish/512", cells: 512},
	{name: "block/28k", cells: 28000},
}

func benchSeenSetCells(n int) ([]*Cell, []Hash) {
	cells := make([]*Cell, n)
	hashes := make([]Hash, n)
	for i := range cells {
		cells[i] = BeginCell().MustStoreUInt(uint64(i), 64).EndCell()
		hashes[i] = cells[i].HashKey()
	}
	return cells, hashes
}

func BenchmarkStorageSeenSetFill(b *testing.B) {
	for _, shape := range benchSeenSetShape {
		cells, hashes := benchSeenSetCells(shape.cells)
		b.Run(shape.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				var set storageSeenSet
				for i := range cells {
					set.addIfAbsent(cells[i], hashes[i])
				}
			}
		})
	}
}

// BenchmarkStorageSeenSetRepeat measures the steady-state probe once the table
// has stopped growing, so a growth-policy change can be shown not to have made
// lookups worse.
func BenchmarkStorageSeenSetRepeat(b *testing.B) {
	cells, hashes := benchSeenSetCells(28000)
	var set storageSeenSet
	for i := range cells {
		set.addIfAbsent(cells[i], hashes[i])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i := range cells {
			if set.addIfAbsent(cells[i], hashes[i]) {
				b.Fatal("cell reported as new on repeat")
			}
		}
	}
}
