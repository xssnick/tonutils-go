package cell

import (
	"bytes"
	"errors"
	"testing"
)

func pageProofTestGraph(root *Cell, loader *countingLazyLoader) *Cell {
	hash := root.HashKey()
	if paged := loader.cells[hash]; paged != nil {
		return paged
	}
	for _, ref := range root.rawRefs() {
		pageProofTestGraph(ref, loader)
	}
	paged := cellWithLazyRefsFromCell(root, loader.LoadCell)
	loader.cells[hash] = paged
	return paged
}

func TestRecordRecursiveReusesLazyBodies(t *testing.T) {
	root := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	for range 8 {
		root = BeginCell().MustStoreRef(root).MustStoreRef(root).EndCell()
	}
	loader := &countingLazyLoader{cells: make(map[Hash]*Cell)}
	paged := pageProofTestGraph(root, loader)
	rs := NewReadSet(paged)
	if err := rs.RecordRecursive(paged); err != nil {
		t.Fatal(err)
	}
	if rs.Size() != 9 || len(loader.calls) != 8 {
		t.Fatalf("recorded=%d loaded=%d, want 9 and 8", rs.Size(), len(loader.calls))
	}
	for hash, calls := range loader.calls {
		if calls != 1 {
			t.Fatalf("cell %x loaded %d times, want 1", hash, calls)
		}
	}
	clear(loader.calls)
	if err := rs.RecordRecursive(paged); err != nil {
		t.Fatal(err)
	}
	if len(loader.calls) != 0 {
		t.Fatalf("recursive recording reloaded %d retained bodies", len(loader.calls))
	}
}

func TestRecordRecursiveCachedBodyStillValidatesLazyBoundary(t *testing.T) {
	child := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	root := BeginCell().MustStoreRef(child).MustStoreRef(child).EndCell()
	loader := &countingLazyLoader{cells: make(map[Hash]*Cell)}
	paged := pageProofTestGraph(root, loader)
	paged.ref(1).setDepthAt(0, child.Depth()+1)
	if err := NewReadSet(paged).RecordRecursive(paged); !errors.Is(err, ErrLazyRefMismatch) {
		t.Fatalf("malformed repeated lazy boundary: got %v, want ErrLazyRefMismatch", err)
	}
	if loader.calls[child.HashKey()] != 1 {
		t.Fatalf("valid cached body loaded %d times, want 1", loader.calls[child.HashKey()])
	}
}

func complementaryProofTestBodies(t *testing.T) (*Cell, *Cell, *Cell) {
	t.Helper()
	left := BeginCell().MustStoreUInt(1, 8).EndCell()
	right := BeginCell().MustStoreUInt(2, 8).EndCell()
	prunedLeft, err := buildPrunedBranchFromCellAtDepth(left, 1, _DataCellMaxLevel, nil)
	if err != nil {
		t.Fatal(err)
	}
	prunedRight, err := buildPrunedBranchFromCellAtDepth(right, 1, _DataCellMaxLevel, nil)
	if err != nil {
		t.Fatal(err)
	}
	return BeginCell().MustStoreRef(prunedLeft).MustStoreRef(right).EndCell(),
		BeginCell().MustStoreRef(left).MustStoreRef(prunedRight).EndCell(),
		BeginCell().MustStoreRef(left).MustStoreRef(right).EndCell()
}

func TestCombineMerkleProofFastReusesLazyBodies(t *testing.T) {
	left, right, full := complementaryProofTestBodies(t)
	for range 8 {
		left = BeginCell().MustStoreRef(left).MustStoreRef(left).EndCell()
		right = BeginCell().MustStoreRef(right).MustStoreRef(right).EndCell()
		full = BeginCell().MustStoreRef(full).MustStoreRef(full).EndCell()
	}
	loader := &countingLazyLoader{cells: make(map[Hash]*Cell)}
	pagedLeft := pageProofTestGraph(left, loader)
	pagedRight := pageProofTestGraph(right, loader)
	combined, err := CombineMerkleProofFastRaw(pagedLeft, pagedRight)
	if err != nil {
		t.Fatal(err)
	}
	if len(loader.calls) != 16 {
		t.Fatalf("loaded %d distinct cells, want 16", len(loader.calls))
	}
	for hash, calls := range loader.calls {
		if calls != 1 {
			t.Fatalf("cell %x loaded %d times, want 1", hash, calls)
		}
	}
	if !bytes.Equal(combined.ToBOC(), full.ToBOC()) {
		t.Fatal("lazy combine differs from the full proof")
	}
	if combined.ref(0) != combined.ref(1) {
		t.Fatal("combined proof lost shared cell identity")
	}
}

func TestCombineMerkleProofFastCachedBodyStillValidatesLazyBoundary(t *testing.T) {
	left, right, _ := complementaryProofTestBodies(t)
	left = BeginCell().MustStoreRef(left).MustStoreRef(left).EndCell()
	right = BeginCell().MustStoreRef(right).MustStoreRef(right).EndCell()
	loader := &countingLazyLoader{cells: make(map[Hash]*Cell)}
	pagedLeft := pageProofTestGraph(left, loader)
	pagedRight := pageProofTestGraph(right, loader)
	malformed := pagedLeft.ref(1)
	malformed.setDepthAt(0, malformed.Depth()+1)
	if _, err := CombineMerkleProofFastRaw(pagedLeft, pagedRight); !errors.Is(err, ErrLazyRefMismatch) {
		t.Fatalf("malformed repeated lazy boundary: got %v, want ErrLazyRefMismatch", err)
	}
}

func TestProofParallelCacheUsesAllShards(t *testing.T) {
	var cache proofParallelCache
	for depth := 0; depth <= _DataCellMaxLevel; depth++ {
		seen := make(map[*proofParallelCacheShard]struct{})
		for i := 0; i < proofParallelCacheShards; i++ {
			var hash Hash
			hash[0] = byte(i)
			seen[cache.shard(proofBodyKey{hash: hash, merkleDepth: depth})] = struct{}{}
		}
		if len(seen) != proofParallelCacheShards {
			t.Fatalf("depth %d reaches %d shards, want %d", depth, len(seen), proofParallelCacheShards)
		}
	}
}

func TestRecordRecursiveRetainedBodyDoesNotHideMissingLoader(t *testing.T) {
	child := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	rs := NewReadSet(child)
	rs.Record(child)
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(child))
	if err := rs.RecordRecursive(lazy); !errors.Is(err, ErrLazyLoaderNotSet) {
		t.Fatalf("missing loader: got %v, want ErrLazyLoaderNotSet", err)
	}
}

func TestRecordRecursiveLoadsDifferentRawVirtualRepresentation(t *testing.T) {
	left, right, full := complementaryProofTestBodies(t)
	loader := &countingLazyLoader{cells: map[Hash]*Cell{right.HashKey(): right}}
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(right), loader.LoadCell).Virtualize(0)
	rs := NewReadSet(full)
	rs.Record(left.Virtualize(0))
	if err := rs.RecordRecursive(lazy); err != nil {
		t.Fatal(err)
	}
	if loader.calls[right.HashKey()] != 1 {
		t.Fatalf("different raw representation loaded %d times, want 1", loader.calls[right.HashKey()])
	}
	if _, recorded := rs.Contains(right.ref(0).HashKey()); !recorded {
		t.Fatal("a recorded root caused its unread descendants to be skipped")
	}
}

func TestCachedLazyLoadKeepsDifferentRawVirtualRepresentation(t *testing.T) {
	left, right, _ := complementaryProofTestBodies(t)
	loader := &countingLazyLoader{cells: map[Hash]*Cell{right.HashKey(): right}}
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(right), loader.LoadCell).Virtualize(0)
	var cache cellLoadCache
	cache.store(left.HashKey(), left)
	loaded, err := loadLazyPrunedRefCached(lazy, &cache)
	if err != nil {
		t.Fatal(err)
	}
	if loader.calls[right.HashKey()] != 1 || loaded.rawCell() != right {
		t.Fatal("cached effective hash replaced a different raw representation")
	}
	if !loaded.IsVirtualized() || loaded.HashKey() != lazy.HashKey() {
		t.Fatal("loading changed the effective boundary view")
	}
}

func TestCombineMerkleProofFastCachedBodyDoesNotHideMissingLoader(t *testing.T) {
	left, right, _ := complementaryProofTestBodies(t)
	left = BeginCell().MustStoreRef(left).MustStoreRef(left).EndCell()
	right = BeginCell().MustStoreRef(right).MustStoreRef(right).EndCell()
	loader := &countingLazyLoader{cells: make(map[Hash]*Cell)}
	pagedLeft := pageProofTestGraph(left, loader)
	pagedRight := pageProofTestGraph(right, loader)
	pagedLeft.ref(1).meta.lazyLoader = nil
	if _, err := CombineMerkleProofFastRaw(pagedLeft, pagedRight); !errors.Is(err, ErrLazyLoaderNotSet) {
		t.Fatalf("missing loader: got %v, want ErrLazyLoaderNotSet", err)
	}
}
