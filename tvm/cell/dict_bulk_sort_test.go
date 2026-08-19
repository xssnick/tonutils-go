package cell

import (
	"bytes"
	"math/rand"
	"testing"
)

func bulkSortKey(rnd *rand.Rand, keySz uint) []byte {
	key := make([]byte, (keySz+7)/8)
	rnd.Read(key)
	if tail := keySz % 8; tail != 0 {
		key[len(key)-1] &= ^byte(0) << (8 - tail)
	}
	return key
}

// TestNewDictFromItemsAcrossRadixThreshold covers the batch sizes the older
// equivalence test stops short of: it tops out at 200 items, while the bulk
// sort only switches to bucket distribution at dictBulkRadixThreshold, so the
// distributing path had no coverage at all.
//
// Sequential SetBuilder calls are the ground truth for both the resulting tree
// and the duplicate rule: the last item of an equal-key run wins, which only
// holds if the sort is stable, and stability is exactly what a distribution
// pass can quietly lose.
func TestNewDictFromItemsAcrossRadixThreshold(t *testing.T) {
	rnd := rand.New(rand.NewSource(20260819))

	type shape struct {
		keySz uint
		count int
	}
	shapes := []shape{
		// straddle the threshold, and go well past it
		{keySz: 32, count: dictBulkRadixThreshold - 1},
		{keySz: 32, count: dictBulkRadixThreshold},
		{keySz: 32, count: dictBulkRadixThreshold + 1},
		{keySz: 256, count: 1000},
		{keySz: 256, count: 4096},
		// a key shorter than the byte the distribution would bucket on: the
		// batch is large but has to take the plain path
		{keySz: 5, count: 1000},
		{keySz: 1, count: 500},
	}

	for _, sh := range shapes {
		items := make([]DictBulkKV, 0, sh.count)
		for i := 0; i < sh.count; i++ {
			items = append(items, DictBulkKV{
				Key:   bulkSortKey(rnd, sh.keySz),
				Value: BeginCell().MustStoreUInt(uint64(i)&0x3FFFF, 34),
			})
		}
		// duplicate a slice of the batch so the last-wins rule is exercised
		for i := 0; i < len(items)/4; i++ {
			src := items[rnd.Intn(len(items))]
			items = append(items, DictBulkKV{
				Key:   append([]byte(nil), src.Key...),
				Value: BeginCell().MustStoreUInt(uint64(0x2AAAA), 34),
			})
		}
		rnd.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })

		sequential := NewDict(sh.keySz)
		for _, kv := range items {
			keyCell := BeginCell().MustStoreSlice(kv.Key, sh.keySz).EndCell()
			if err := sequential.SetBuilder(keyCell, kv.Value); err != nil {
				t.Fatalf("keySz=%d count=%d: sequential set: %v", sh.keySz, len(items), err)
			}
		}

		bulk, err := NewDictFromItems(sh.keySz, append([]DictBulkKV(nil), items...))
		if err != nil {
			t.Fatalf("keySz=%d count=%d: bulk build: %v", sh.keySz, len(items), err)
		}

		want, got := sequential.AsCell(), bulk.AsCell()
		if (want == nil) != (got == nil) {
			t.Fatalf("keySz=%d count=%d: bulk root %v, sequential %v", sh.keySz, len(items), got, want)
		}
		if want != nil && !bytes.Equal(want.Hash(), got.Hash()) {
			t.Fatalf("keySz=%d count=%d: bulk root %x differs from sequential %x",
				sh.keySz, len(items), got.Hash(), want.Hash())
		}
	}
}

// TestSortDictBulkItemsIsStable pins the property the duplicate rule rests on
// directly, on both sides of the threshold: equal keys must come out in the
// order they went in.
func TestSortDictBulkItemsIsStable(t *testing.T) {
	for _, count := range []int{dictBulkRadixThreshold - 1, dictBulkRadixThreshold, 4096} {
		rnd := rand.New(rand.NewSource(int64(count)))
		items := make([]DictBulkKV, count)
		// draw keys from a small pool so equal-key runs are long
		pool := make([][]byte, 8)
		for i := range pool {
			pool[i] = bulkSortKey(rnd, 256)
		}
		for i := range items {
			items[i] = DictBulkKV{
				Key:   pool[rnd.Intn(len(pool))],
				Value: BeginCell().MustStoreUInt(uint64(i), 34),
			}
		}

		order := make(map[*Builder]int, count)
		for i := range items {
			order[items[i].Value] = i
		}

		sortDictBulkItems(items, 256)

		for i := 1; i < len(items); i++ {
			if compareDictBulkKeys(items[i-1].Key, items[i].Key, 256) > 0 {
				t.Fatalf("count=%d: batch is not sorted at %d", count, i)
			}
			if compareDictBulkKeys(items[i-1].Key, items[i].Key, 256) == 0 &&
				order[items[i-1].Value] > order[items[i].Value] {
				t.Fatalf("count=%d: equal keys reordered at %d", count, i)
			}
		}
	}
}

// TestBulkBuildFallsBackWhenArenaIsSpent pins the arena's escape hatch. The
// slabs are sized from an estimate, so a build that outgrows either of them
// has to finish through ordinary allocation and produce the very same tree —
// otherwise a mis-estimate would be a correctness bug rather than a slowdown.
func TestBulkBuildFallsBackWhenArenaIsSpent(t *testing.T) {
	const count = 600
	items := make([]DictBulkKV, count)
	for i := range items {
		key := make([]byte, 32)
		key[0] = byte(i >> 8)
		key[1] = byte(i)
		key[31] = byte(i)
		items[i] = DictBulkKV{Key: key, Value: BeginCell().MustStoreUInt(uint64(i), 34)}
	}

	reference, err := NewDictFromItems(256, append([]DictBulkKV(nil), items...))
	if err != nil {
		t.Fatalf("reference build: %v", err)
	}

	// Every arena that covers only part of the build, including none of it.
	for _, arena := range []*dictBuildArena{
		nil,
		newDictBuildArena(1, 1),
		newDictBuildArena(count, 64),
		newDictBuildArena(4, dictBulkArenaDataBytes(items, 256)),
		newDictBuildArena(2*count-1, 128),
	} {
		sorted := append([]DictBulkKV(nil), items...)
		sortDictBulkItems(sorted, 256)
		keyCells := make([]Cell, len(sorted))
		for i := range sorted {
			keyCells[i] = Cell{data: sorted[i].Key, bitsSz: 256}
		}

		d := NewDict(256)
		root, err := d.buildFromSorted(sorted, keyCells, 0, arena)
		if err != nil {
			t.Fatalf("build with a spent arena: %v", err)
		}
		if !bytes.Equal(root.Hash(), reference.AsCell().Hash()) {
			t.Fatalf("build with a spent arena produced %x, want %x", root.Hash(), reference.AsCell().Hash())
		}
	}
}
