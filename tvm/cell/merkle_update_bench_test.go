package cell

import (
	"fmt"
	"math/rand"
	"testing"
)

type merkleUpdateLargeDictCase struct {
	from   *Cell
	to     *Cell
	update *Cell
}

type benchmarkMerkleUpdateApplier struct {
	ready      map[merkleUpdateVisitKey]*Cell
	reused     map[Hash]struct{}
	reusedRefs map[merkleUpdateReusedRefKey]struct{}
	reusedList []MerkleUpdateReusedCell
	refList    []MerkleUpdateReusedRef
}

func TestApplyMerkleUpdateLargeDictFivePercentChanges(t *testing.T) {
	tc := newMerkleUpdateLargeDictCase(t, 4096, 5, 2026042601)

	got, reused, err := ApplyMerkleUpdate(tc.from, tc.update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if got.HashKey() != tc.to.HashKey() {
		t.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), tc.to.Hash())
	}
	if len(reused.Cells) == 0 {
		t.Fatal("expected reused cells for 5 percent update")
	}
	if len(reused.Refs) == 0 {
		t.Fatal("expected reused refs for 5 percent update")
	}
}

func BenchmarkApplyMerkleUpdateLargeDictFivePercentChanges(b *testing.B) {
	tc := newMerkleUpdateLargeDictCase(b, 16384, 5, 2026042602)

	b.Run("entries_16384_changes_5pct/baseline_hash_memo", func(b *testing.B) {
		benchmarkApplyMerkleUpdate(b, tc, benchmarkApplyMerkleUpdateBaseline)
	})
	b.Run("entries_16384_changes_5pct/optimized", func(b *testing.B) {
		benchmarkApplyMerkleUpdate(b, tc, ApplyMerkleUpdate)
	})
}

func benchmarkApplyMerkleUpdate(
	b *testing.B,
	tc merkleUpdateLargeDictCase,
	apply func(from, update *Cell) (*Cell, MerkleUpdateReuse, error),
) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		got, reused, err := apply(tc.from, tc.update)
		if err != nil {
			b.Fatal(err)
		}
		if got.HashKey() != tc.to.HashKey() {
			b.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), tc.to.Hash())
		}
		if len(reused.Cells) == 0 || len(reused.Refs) == 0 {
			b.Fatalf("expected reuse metadata, got cells=%d refs=%d", len(reused.Cells), len(reused.Refs))
		}
	}
}

func newMerkleUpdateLargeDictCase(tb testing.TB, entries, changePercent int, seed int64) merkleUpdateLargeDictCase {
	tb.Helper()

	rnd := rand.New(rand.NewSource(seed))
	fromDict := NewDict(64)
	keys := make([]uint64, 0, entries)
	used := make(map[uint64]struct{}, entries)
	for len(keys) < entries {
		key := rnd.Uint64()
		if _, ok := used[key]; ok {
			continue
		}
		used[key] = struct{}{}
		keys = append(keys, key)

		if err := fromDict.Set(merkleUpdateBenchKey(key), merkleUpdateBenchValue(key^0x55aa55aa55aa55aa)); err != nil {
			tb.Fatalf("failed to set source dict item: %v", err)
		}
	}

	toDict := fromDict.Copy()
	changes := entries * changePercent / 100
	if changes < 1 {
		changes = 1
	}

	perm := rnd.Perm(entries)
	for i := 0; i < changes; i++ {
		key := keys[perm[i]]
		next := key ^ uint64(i+1)*0x9e3779b97f4a7c15
		if err := toDict.Set(merkleUpdateBenchKey(key), merkleUpdateBenchValue(next)); err != nil {
			tb.Fatalf("failed to set destination dict item: %v", err)
		}
	}

	from := fromDict.AsCell()
	to := toDict.AsCell()
	updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(from, to)
	if err != nil {
		tb.Fatalf("failed to build merkle update bodies: %v", err)
	}

	return merkleUpdateLargeDictCase{
		from:   from,
		to:     to,
		update: mustMerkleUpdateCell(tb, updateFrom, updateTo),
	}
}

func merkleUpdateBenchKey(value uint64) *Cell {
	return BeginCell().MustStoreUInt(value, 64).EndCell()
}

func merkleUpdateBenchValue(value uint64) *Cell {
	return BeginCell().MustStoreUInt(value, 64).EndCell()
}

func benchmarkApplyMerkleUpdateBaseline(from, update *Cell) (*Cell, MerkleUpdateReuse, error) {
	if from == nil {
		return nil, MerkleUpdateReuse{}, fmt.Errorf("from cell is nil")
	}
	if from.Level() != 0 {
		return nil, MerkleUpdateReuse{}, fmt.Errorf("roots have non-zero level")
	}

	updateFrom, updateTo, err := merkleUpdateRootRefs(update, true)
	if err != nil {
		return nil, MerkleUpdateReuse{}, err
	}
	if from.HashKey(0) != updateFrom.HashKey(0) {
		return nil, MerkleUpdateReuse{}, fmt.Errorf("invalid Merkle update")
	}

	known := map[Hash]struct{}{}
	if err = benchmarkWalkMerkleUpdateSource(
		updateFrom,
		updateFrom,
		0,
		map[merkleUpdateVisitKey]struct{}{},
		false,
		func(source *Cell, merkleDepth int) {
			known[source.HashKey(merkleDepth)] = struct{}{}
		},
	); err != nil {
		return nil, MerkleUpdateReuse{}, err
	}

	applier := benchmarkMerkleUpdateApplier{
		ready:      map[merkleUpdateVisitKey]*Cell{},
		reused:     map[Hash]struct{}{},
		reusedRefs: map[merkleUpdateReusedRefKey]struct{}{},
	}
	root, _, err := benchmarkCollectMerkleUpdateReuse(updateTo, 0, known, &applier)
	if err != nil {
		return nil, MerkleUpdateReuse{}, err
	}

	return root, MerkleUpdateReuse{
		Cells: applier.reusedList,
		Refs:  applier.refList,
	}, nil
}

func benchmarkWalkMerkleUpdateSource(
	source, shape *Cell,
	merkleDepth int,
	visited map[merkleUpdateVisitKey]struct{},
	validateShape bool,
	onKnown func(source *Cell, merkleDepth int),
) error {
	if source == nil || shape == nil {
		return fmt.Errorf("merkle update contains nil reference")
	}

	key := merkleUpdateSeenKey(shape, merkleDepth)
	if _, ok := visited[key]; ok {
		return nil
	}
	visited[key] = struct{}{}

	if validateShape {
		if err := validateLoadedCell(shape); err != nil {
			return fmt.Errorf("invalid merkle update source subtree: %w", err)
		}
	}

	onKnown(source, merkleDepth)
	if shape.GetType() == PrunedCellType {
		return nil
	}
	if source.refsCount() != shape.refsCount() {
		return fmt.Errorf("invalid merkle update: source subtree refs mismatch")
	}

	sourceRefs := newCellRefView(source)
	shapeRefs := newCellRefView(shape)
	childDepth := merkleChildDepth(shape, merkleDepth)
	for i := 0; i < source.refsCount(); i++ {
		shapeRef, err := shapeRefs.boundaryRef(i)
		if err != nil {
			return fmt.Errorf("failed to peek shape ref %d: %w", i, err)
		}
		shapeRef, err = shapeRef.load()
		if err != nil {
			return fmt.Errorf("failed to load shape ref %d: %w", i, err)
		}

		sourceRef, err := merkleUpdateSourceTreeRef(&sourceRefs, shapeRef, i)
		if err != nil {
			return err
		}
		if err := benchmarkWalkMerkleUpdateSource(sourceRef, shapeRef, childDepth, visited, validateShape, onKnown); err != nil {
			return err
		}
	}

	return nil
}

func benchmarkCollectMerkleUpdateReuse(
	cell *Cell,
	merkleDepth int,
	known map[Hash]struct{},
	reuse *benchmarkMerkleUpdateApplier,
) (*Cell, *merkleUpdateReusedChild, error) {
	if cell == nil {
		return nil, nil, fmt.Errorf("merkle update contains nil reference")
	}

	if hash, ok := merkleUpdatePrunedBoundaryHash(cell, merkleDepth); ok {
		if _, ok := known[hash]; !ok {
			return nil, nil, fmt.Errorf("unknown pruned branch %x", hash[:])
		}
		ref := cell.Virtualize(uint8(merkleDepth))
		reuse.addReusedCell(hash, ref)
		return ref, &merkleUpdateReusedChild{hash: hash, raw: ref}, nil
	}
	if cell.GetType() == PrunedCellType {
		return cell, nil, nil
	}

	key := merkleUpdateSeenKey(cell, merkleDepth)
	if ready, ok := reuse.ready[key]; ok {
		return ready, nil, nil
	}
	if cell.refsCount() == 0 {
		reuse.ready[key] = cell
		return cell, nil, nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:cell.refsCount()]
	var reusedRefsBuf [4]*merkleUpdateReusedChild
	reusedRefs := reusedRefsBuf[:0]
	refView := newCellRefView(cell)
	childDepth := merkleChildDepth(cell, merkleDepth)
	for i := 0; i < len(refs); i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to peek destination ref %d: %w", i, err)
		}
		ref, err = ref.load()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load destination ref %d: %w", i, err)
		}
		rebuilt, reusedRef, err := benchmarkCollectMerkleUpdateReuse(ref, childDepth, known, reuse)
		if err != nil {
			return nil, nil, err
		}
		refs[i] = rebuilt
		reusedRefs = append(reusedRefs, reusedRef)
	}

	rebuilt, _, err := refView.cloneWithRefs(refs, nil)
	if err != nil {
		return nil, nil, err
	}
	parentHash := rebuilt.HashKey()
	for i, reusedRef := range reusedRefs {
		if reusedRef != nil {
			reuse.addReusedRef(parentHash, i, reusedRef.hash, reusedRef.raw)
		}
	}
	reuse.ready[key] = rebuilt
	return rebuilt, nil, nil
}

func (a *benchmarkMerkleUpdateApplier) addReusedCell(hash Hash, raw *Cell) {
	if _, ok := a.reused[hash]; ok {
		return
	}
	a.reused[hash] = struct{}{}
	a.reusedList = append(a.reusedList, MerkleUpdateReusedCell{
		Hash: hash,
		Cell: raw,
	})
}

func (a *benchmarkMerkleUpdateApplier) addReusedRef(parentHash Hash, refIndex int, logicalHash Hash, raw *Cell) {
	key := merkleUpdateReusedRefKey{parentHash: parentHash, refIndex: refIndex, logicalHash: logicalHash}
	if _, ok := a.reusedRefs[key]; ok {
		return
	}
	a.reusedRefs[key] = struct{}{}
	a.refList = append(a.refList, MerkleUpdateReusedRef{
		ParentHash:  parentHash,
		RefIndex:    refIndex,
		LogicalHash: logicalHash,
		RawCell:     raw,
	})
}
