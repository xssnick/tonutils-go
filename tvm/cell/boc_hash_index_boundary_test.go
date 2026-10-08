package cell

import (
	"encoding/binary"
	"testing"
)

// bocHashIndexProbeItems builds count cells with pairwise distinct hashes, and
// therefore pairwise distinct fingerprints in the low four bytes.
func bocHashIndexProbeItems(count int) []bocSerializeItem {
	items := make([]bocSerializeItem, count)
	for i := range items {
		var hash Hash
		binary.LittleEndian.PutUint32(hash[:4], uint32(i)*2654435761+1)
		binary.LittleEndian.PutUint32(hash[4:8], uint32(i))
		items[i].cell = &Cell{hash0: hash}
	}

	return items
}

// The index sizes itself to the first power of two whose three-quarter load
// point covers the hint, and grows the moment one more entry arrives. Those two
// facts meet at exactly 3/4 of a power of two, which is where a presize either
// fits its hint exactly or wastes a doubling — and it is the boundary an earlier
// review asked for by name, because a hint that lands one entry past it
// allocates twice the table for one cell.
//
// Both sides are pinned here: at the boundary the table is sized once and never
// rehashes while the hinted entries go in, and one past it the table is a
// doubling larger from the start. Correctness is checked on both, since a growth
// that dropped an entry would otherwise show up only as a duplicated cell in
// somebody's BoC.
func TestBOCHashIndexPowerOfTwoBoundary(t *testing.T) {
	for capacity := bocHashIndexInitialCapacity; capacity <= 1<<13; capacity *= 2 {
		exact := capacity * 3 / 4

		index := newBOCHashIndex(exact)
		if got := len(index.entries); got != capacity {
			t.Fatalf("hint %d (3/4 of %d) sized the table at %d, want %d",
				exact, capacity, got, capacity)
		}
		if index.growAt != exact {
			t.Fatalf("hint %d sized the table to grow at %d, want %d", exact, index.growAt, exact)
		}

		items := bocHashIndexProbeItems(exact + 1)
		for i := range exact {
			index.set(bocHashFingerprint(items[i].cell.getHash(_DataCellMaxLevel)), uint32(i))
		}
		if got := len(index.entries); got != capacity {
			t.Fatalf("hint %d rehashed while its own %d entries went in: table is %d, want %d",
				exact, exact, got, capacity)
		}
		for i := range exact {
			hash := items[i].cell.getHash(_DataCellMaxLevel)
			if got, ok := index.get(bocHashFingerprint(hash), hash, items[i].cell, items); !ok || got != uint32(i) {
				t.Fatalf("hint %d: entry %d lookup failed at the boundary: got %d, found %t",
					exact, i, got, ok)
			}
		}

		// One entry past the boundary is where the table doubles. It must still
		// resolve everything that was in it before the rehash.
		index.set(bocHashFingerprint(items[exact].cell.getHash(_DataCellMaxLevel)), uint32(exact))
		if got := len(index.entries); got != 2*capacity {
			t.Fatalf("hint %d: the entry past the boundary sized the table at %d, want %d",
				exact, got, 2*capacity)
		}
		for i := range exact + 1 {
			hash := items[i].cell.getHash(_DataCellMaxLevel)
			if got, ok := index.get(bocHashFingerprint(hash), hash, items[i].cell, items); !ok || got != uint32(i) {
				t.Fatalf("hint %d: entry %d lookup failed after growth: got %d, found %t",
					exact, i, got, ok)
			}
		}

		// And a hint one past the boundary presizes the doubled table up front
		// rather than sizing for the boundary and rehashing on the way in.
		if got := len(newBOCHashIndex(exact + 1).entries); got != 2*capacity {
			t.Fatalf("hint %d sized the table at %d, want %d", exact+1, got, 2*capacity)
		}
	}
}

// A presize must never make the serializer emit different bytes, whatever the
// hint does to the table underneath — including at the boundary, one either side
// of it, and far past the point where the hint is honoured at all.
func TestBOCHashIndexBoundaryHintsChangeNoByte(t *testing.T) {
	shared := BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	left := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(shared).EndCell()
	right := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(shared).EndCell()
	root := BeginCell().MustStoreRef(left).MustStoreRef(right).EndCell()

	want := root.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true})
	for capacity := bocHashIndexInitialCapacity; capacity <= 1<<13; capacity *= 2 {
		for _, hint := range []int{capacity*3/4 - 1, capacity * 3 / 4, capacity*3/4 + 1, capacity} {
			got := root.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true, CellsCountHint: hint})
			if string(want) != string(got) {
				t.Fatalf("hint %d changed the boc", hint)
			}
		}
	}
}
