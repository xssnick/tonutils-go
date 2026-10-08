package cell

import "testing"

func TestCellLoadCacheHandlesCollisionsAndSpill(t *testing.T) {
	var cells [96]Cell
	var hashes [96]Hash
	for i := range cells {
		// The same fingerprint forces a full collision chain; full hashes still
		// distinguish every entry before and after the cache spills.
		hashes[i][4] = byte(i + 1)
		cells[i].setHashAt(0, hashes[i][:])
	}
	var cache cellLoadCache
	for i := range cells {
		if cache.lookup(hashes[i]) != nil {
			t.Fatal("new hash was already cached")
		}
		cache.store(hashes[i], &cells[i])
		for j := 0; j <= i; j++ {
			if cache.lookup(hashes[j]) != &cells[j] {
				t.Fatalf("insert %d lost entry %d", i, j)
			}
		}
	}
	if cache.lookup(Hash{}) != nil {
		t.Fatal("missing hash matched a colliding entry")
	}
}

func TestCellLoadCacheSmallTraversalDoesNotAllocate(t *testing.T) {
	var cells [32]Cell
	var hashes [32]Hash
	for i := range cells {
		hashes[i][0] = byte(i + 1)
		cells[i].setHashAt(0, hashes[i][:])
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		var cache cellLoadCache
		for i := range cells {
			cache.store(hashes[i], &cells[i])
		}
		for i := range cells {
			if cache.lookup(hashes[i]) != &cells[i] {
				panic("missing cached cell")
			}
		}
	}); allocs != 0 {
		t.Fatalf("small traversal allocated %.0f times, want 0", allocs)
	}
}
