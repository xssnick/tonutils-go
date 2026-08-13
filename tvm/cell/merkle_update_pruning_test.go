package cell

import (
	"math/rand"
	"testing"
)

// The dictionary fixtures elsewhere in this package rebuild every cell of the
// destination from a builder, so no destination subtree is one the source ever
// held, the prune predicate never recognizes a hash, and nothing is pruned at
// all. That makes them useless for anything about merkle updates that matters: a
// proof with no pruned boundaries exercises neither the boundary bookkeeping, nor
// the source proof, nor the substitution that lets the applied root share the old
// state's memory instead of copying it.
//
// A collator does something different. It rebuilds the paths it touched and
// leaves everything else as the cells it read, so untouched subtrees can be
// pruned against the source. This fixture reproduces that, and with it the shape
// of a real block is matched within about ten percent on every counter that
// drives the cost.

// prunedUpdateTree is a balanced binary tree with distinct leaf payloads, the
// shape a state trie keyed by account id takes.
func prunedUpdateTree(depth int, path uint64) *Cell {
	if depth == 0 {
		return BeginCell().MustStoreUInt(path, 64).
			MustStoreRef(BeginCell().MustStoreUInt(path*2654435761, 64).EndCell()).
			EndCell()
	}
	return BeginCell().MustStoreUInt(uint64(depth), 8).
		MustStoreRef(prunedUpdateTree(depth-1, path<<1)).
		MustStoreRef(prunedUpdateTree(depth-1, path<<1|1)).
		EndCell()
}

// rebuildTouchedPaths returns the destination: touched leaves are new cells,
// every subtree holding nothing touched is returned as-is — a cell the recorder
// knows, so the update can prune it.
func rebuildTouchedPaths(tb testing.TB, c *Cell, depth int, touched map[uint64]struct{}, path uint64) *Cell {
	tb.Helper()

	if depth == 0 {
		if _, ok := touched[path]; !ok {
			return c
		}
		return BeginCell().MustStoreUInt(path, 64).
			MustStoreRef(BeginCell().MustStoreUInt(path*777, 64).EndCell()).
			EndCell()
	}

	hit := false
	for key := range touched {
		if key>>uint(depth) == path {
			hit = true
			break
		}
	}
	if !hit {
		return c
	}

	slice, err := c.BeginParse()
	if err != nil {
		tb.Fatal(err)
	}
	marker, err := slice.LoadUInt(8)
	if err != nil {
		tb.Fatal(err)
	}
	left, err := slice.LoadRef()
	if err != nil {
		tb.Fatal(err)
	}
	right, err := slice.LoadRef()
	if err != nil {
		tb.Fatal(err)
	}
	return BeginCell().MustStoreUInt(marker, 8).
		MustStoreRef(rebuildTouchedPaths(tb, left.MustToCell(), depth-1, touched, path<<1)).
		MustStoreRef(rebuildTouchedPaths(tb, right.MustToCell(), depth-1, touched, path<<1|1)).
		EndCell()
}

type prunedUpdateFixture struct {
	read *ReadSet
	from *Cell
	to   *Cell
}

func newPrunedUpdateFixture(tb testing.TB, depth, touches int, seed int64) prunedUpdateFixture {
	tb.Helper()

	read := NewReadSet(prunedUpdateTree(depth, 0))

	rnd := rand.New(rand.NewSource(seed))
	touched := make(map[uint64]struct{}, touches)
	for len(touched) < touches {
		touched[uint64(rnd.Intn(1<<uint(depth)))] = struct{}{}
	}

	from := read.Root()
	return prunedUpdateFixture{
		read: read,
		from: from,
		to:   rebuildTouchedPaths(tb, from, depth, touched, 0),
	}
}

// This is the guard the byte-identity checks cannot provide. An update that
// stopped substituting source subtrees would still produce the same bytes and
// the same hashes, while the new state quietly stopped pointing at the old one
// and doubled the node's resident memory instead.
func TestCreateMerkleUpdateAppliedSharesUntouchedSubtrees(t *testing.T) {
	for _, tc := range []struct {
		name    string
		depth   int
		touches int
	}{
		{name: "sparse", depth: 12, touches: 200},
		{name: "block-shaped", depth: 14, touches: 345},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPrunedUpdateFixture(t, tc.depth, tc.touches, 20260809)
			update, applied, err := fixture.read.CreateMerkleUpdateApplied(fixture.to)
			if err != nil {
				t.Fatal(err)
			}
			if applied.HashKey() != fixture.to.HashKey() {
				t.Fatalf("applied root %x, want %x", applied.Hash()[:8], fixture.to.Hash()[:8])
			}

			source := fixture.read.Source()
			stepped, err := ApplyMerkleUpdate(source, update)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if stepped.HashKey() != applied.HashKey() {
				t.Fatalf("applying the update gives %x, building it applied gives %x",
					stepped.Hash()[:8], applied.Hash()[:8])
			}

			skipWhileAppliedRootIsTraced(t, applied)
			shared := countSharedCells(applied, source)
			// Touching k leaves of a depth-d tree leaves the overwhelming
			// majority of subtrees untouched, so sharing has to be on the order
			// of the touched paths, not a handful of cells.
			if shared < tc.touches {
				t.Fatalf("applied root shares only %d cells with the source over %d touched leaves",
					shared, tc.touches)
			}
			if want := countSharedCells(stepped, source); shared < want {
				t.Fatalf("applied root shares %d cells, the stepwise apply shares %d", shared, want)
			}
			assertNoTrace(t, applied)
			assertNoTrace(t, update)
		})
	}
}

func BenchmarkCreateMerkleUpdateAppliedBlockShaped(b *testing.B) {
	fixture := newPrunedUpdateFixture(b, 14, 345, 20260809)
	if _, _, err := fixture.read.CreateMerkleUpdateApplied(fixture.to); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		update, applied, err := fixture.read.CreateMerkleUpdateApplied(fixture.to)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkCellSink = update
		benchmarkCellSink = applied
	}
}
