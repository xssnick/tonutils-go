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
	ready map[merkleUpdateVisitKey]*Cell
}

func TestApplyMerkleUpdateLargeDictFivePercentChanges(t *testing.T) {
	tc := newMerkleUpdateLargeDictCase(t, 4096, 5, 2026042601)

	got, err := ApplyMerkleUpdate(tc.from, tc.update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if got.HashKey() != tc.to.HashKey() {
		t.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), tc.to.Hash())
	}
	// Only five percent of the dictionary moved, so the applied root must stand
	// on the source's own cells rather than on copies of them.
	if countSharedCells(got, tc.from) == 0 {
		t.Fatal("applied root shares no cell with the source, so every unchanged subtree was copied")
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
	apply func(from, update *Cell) (*Cell, error),
) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		got, err := apply(tc.from, tc.update)
		if err != nil {
			b.Fatal(err)
		}
		if got.HashKey() != tc.to.HashKey() {
			b.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), tc.to.Hash())
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

func benchmarkApplyMerkleUpdateBaseline(from, update *Cell) (*Cell, error) {
	if from == nil {
		return nil, fmt.Errorf("from cell is nil")
	}
	if from.Level() != 0 {
		return nil, fmt.Errorf("roots have non-zero level")
	}

	updateFrom, updateTo, err := merkleUpdateRootRefs(update, true)
	if err != nil {
		return nil, err
	}
	if from.HashKey(0) != updateFrom.HashKey(0) {
		return nil, fmt.Errorf("invalid Merkle update")
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
		return nil, err
	}

	applier := benchmarkMerkleUpdateApplier{ready: map[merkleUpdateVisitKey]*Cell{}}
	return benchmarkCollectMerkleUpdateReuse(updateTo, 0, known, &applier)
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
) (*Cell, error) {
	if cell == nil {
		return nil, fmt.Errorf("merkle update contains nil reference")
	}

	if hash, ok := merkleUpdatePrunedBoundaryHash(cell, merkleDepth); ok {
		if _, ok := known[hash]; !ok {
			return nil, fmt.Errorf("unknown pruned branch %x", hash[:])
		}
		return cell.Virtualize(uint8(merkleDepth)), nil
	}
	if cell.GetType() == PrunedCellType {
		return cell, nil
	}

	key := merkleUpdateSeenKey(cell, merkleDepth)
	if ready, ok := reuse.ready[key]; ok {
		return ready, nil
	}
	if cell.refsCount() == 0 {
		reuse.ready[key] = cell
		return cell, nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:cell.refsCount()]
	refView := newCellRefView(cell)
	childDepth := merkleChildDepth(cell, merkleDepth)
	for i := 0; i < len(refs); i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return nil, fmt.Errorf("failed to peek destination ref %d: %w", i, err)
		}
		ref, err = ref.load()
		if err != nil {
			return nil, fmt.Errorf("failed to load destination ref %d: %w", i, err)
		}
		rebuilt, err := benchmarkCollectMerkleUpdateReuse(ref, childDepth, known, reuse)
		if err != nil {
			return nil, err
		}
		refs[i] = rebuilt
	}

	rebuilt, _, err := refView.cloneWithRefs(refs, nil)
	if err != nil {
		return nil, err
	}
	reuse.ready[key] = rebuilt
	return rebuilt, nil
}

// BenchmarkPreparedMerkleUpdate is the A/B the fused path exists for, on the
// two shapes the node actually runs.
//
// "validate_apply" is one validation and one apply, which is what the
// non-validated chain-state path and the remote proof-backed path each do.
// "validate_apply_validate_apply" is the full-collated live path as it was:
// the candidate is validated and applied against the proof, then validated
// again and applied against the full parent this node holds.
func BenchmarkPreparedMerkleUpdate(b *testing.B) {
	tc := newMerkleUpdateLargeDictCase(b, 16384, 5, 2026081401)
	second, err := FromBOC(tc.from.ToBOC())
	if err != nil {
		b.Fatalf("build the second parent: %v", err)
	}

	b.Run("one_apply/separate", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := ValidateMerkleUpdate(tc.update); err != nil {
				b.Fatal(err)
			}
			if _, err := ApplyMerkleUpdate(tc.from, tc.update); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("one_apply/prepared", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			prepared, err := PrepareMerkleUpdatePlanned(tc.update)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := prepared.ApplyTo(tc.from); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("two_parents/separate", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := ValidateMerkleUpdate(tc.update); err != nil {
				b.Fatal(err)
			}
			if _, err := ApplyMerkleUpdate(tc.from, tc.update); err != nil {
				b.Fatal(err)
			}
			if err := ValidateMerkleUpdate(tc.update); err != nil {
				b.Fatal(err)
			}
			if _, err := ApplyMerkleUpdate(second, tc.update); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("two_parents/prepared", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			prepared, err := PrepareMerkleUpdatePlanned(tc.update)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := prepared.ApplyTo(tc.from); err != nil {
				b.Fatal(err)
			}
			if _, err := prepared.ApplyTo(second); err != nil {
				b.Fatal(err)
			}
		}
	})
	// The shape candidate validation actually runs on the proof-backed shard
	// path: the first apply happens before the verdict is known — it is in the
	// stage that runs ahead of the masterchain view, where a lagging node
	// abandons attempts and must not be made to walk the update — so it is the
	// plain one, and only the second apply, onto the live parent the caller
	// holds, replays the plans. It sits between the two above: it keeps the
	// second apply's saving and gives up the first one's.
	b.Run("two_parents/prepared_second_only", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := ApplyMerkleUpdate(tc.from, tc.update); err != nil {
				b.Fatal(err)
			}
			prepared, err := PrepareMerkleUpdatePlanned(tc.update)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := prepared.ApplyTo(second); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("verdict_only/separate", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := ValidateMerkleUpdate(tc.update); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("verdict_only/prepared", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := PrepareMerkleUpdate(tc.update); err != nil {
				b.Fatal(err)
			}
		}
	})
}
