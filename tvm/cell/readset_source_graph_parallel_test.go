package cell

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

// readEverything parses the whole tree through the recording trace, so every
// cell is read and the graph covers the full source.
func readEverything(tb testing.TB, c *Cell) {
	tb.Helper()
	var walk func(c *Cell) error
	walk = func(c *Cell) error {
		slice, err := c.BeginParse()
		if err != nil {
			return err
		}
		for slice.RefsNum() > 0 {
			child, err := slice.LoadRefCell()
			if err != nil {
				return err
			}
			if err = walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(c); err != nil {
		tb.Fatal(err)
	}
}

// readSome walks a sample of dictionary keys, leaving the rest unread, so the
// graph has a real unread fringe.
func readSome(tb testing.TB, rs *ReadSet, keys []uint64, sample int, seed int64) {
	tb.Helper()
	dict, err := rs.Root().BeginParse()
	if err != nil {
		tb.Fatal(err)
	}
	rebuilt := dict.MustToCell().AsDict(64)
	rnd := rand.New(rand.NewSource(seed))
	for i := 0; i < sample; i++ {
		if _, err := rebuilt.LoadValue(merkleUpdateDictKey(keys[rnd.Intn(len(keys))])); err != nil {
			tb.Fatal(err)
		}
	}
}

func assertSameGraph(t *testing.T, name string, sequential, parallel *sourceGraph) {
	t.Helper()
	if len(sequential.nodes) != len(parallel.nodes) {
		t.Fatalf("%s: parallel graph has %d nodes, sequential %d", name, len(parallel.nodes), len(sequential.nodes))
	}
	if len(sequential.edges) != len(parallel.edges) {
		t.Fatalf("%s: parallel graph has %d edges, sequential %d", name, len(parallel.edges), len(sequential.edges))
	}
	for i := range sequential.nodes {
		s, p := &sequential.nodes[i], &parallel.nodes[i]
		sHash, pHash := s.cell.HashKey(), p.cell.HashKey()
		if sHash != pHash || s.read != p.read ||
			s.firstRef != p.firstRef || s.refCount != p.refCount {
			t.Fatalf("%s: node %d differs: sequential {%x read=%v ref=%d+%d}, parallel {%x read=%v ref=%d+%d}",
				name, i,
				sHash[:6], s.read, s.firstRef, s.refCount,
				pHash[:6], p.read, p.firstRef, p.refCount)
		}
	}
	for i := range sequential.edges {
		if sequential.edges[i] != parallel.edges[i] {
			t.Fatalf("%s: edge %d differs: sequential %d, parallel %d", name, i, sequential.edges[i], parallel.edges[i])
		}
	}
}

// TestParallelSourceGraphMatchesSequential is the byte gate of the parallel
// build: on every shape — deep, shallow, shared across workers' subtrees,
// shared between the spine and a subtree interior, with and without an unread
// fringe — the parallel walk must produce the exact node numbering the
// sequential walk produces, because the claim pass downstream breaks ties by
// index and the update's bytes follow.
func TestParallelSourceGraphMatchesSequential(t *testing.T) {
	shapes := []struct {
		name            string
		mustParallelize bool
		build           func(t *testing.T) *ReadSet
	}{
		{"dict fully read", true, func(t *testing.T) *ReadSet {
			from, _ := merkleUpdateSourceDict(t, 4096, 2026082010)
			rs := NewReadSet(from)
			readEverything(t, rs.Root())
			return rs
		}},
		{"dict with unread fringe", true, func(t *testing.T) *ReadSet {
			from, keys := merkleUpdateSourceDict(t, 8192, 2026082011)
			rs := NewReadSet(from)
			readSome(t, rs, keys, 1500, 2026082012)
			return rs
		}},
		{"shallow tree declines", false, func(t *testing.T) *ReadSet {
			root := BeginCell().
				MustStoreRef(BeginCell().MustStoreUInt(1, 64).EndCell()).
				MustStoreRef(BeginCell().MustStoreUInt(2, 64).EndCell()).
				EndCell()
			rs := NewReadSet(root)
			readEverything(t, rs.Root())
			return rs
		}},
		{"subtree shared across branches", true, func(t *testing.T) *ReadSet {
			shared, _ := merkleUpdateSourceDict(t, 512, 2026082013)
			chain := func(depth int, tag uint64, tail *Cell) *Cell {
				for i := depth; i > 0; i-- {
					tail = BeginCell().MustStoreUInt(tag+uint64(i), 64).MustStoreRef(tail).EndCell()
				}
				return tail
			}
			// The same subtree hangs deep inside two separate branches, so two
			// workers walk it and the merge must keep exactly one copy.
			root := BeginCell().
				MustStoreRef(chain(14, 0x1000, shared)).
				MustStoreRef(chain(17, 0x2000, shared)).
				EndCell()
			rs := NewReadSet(root)
			readEverything(t, rs.Root())
			return rs
		}},
		{"subtree shared between spine and interior", true, func(t *testing.T) *ReadSet {
			shared, _ := merkleUpdateSourceDict(t, 256, 2026082014)
			deep := shared
			for i := 0; i < 18; i++ {
				deep = BeginCell().MustStoreUInt(uint64(0x3000+i), 64).MustStoreRef(deep).EndCell()
			}
			// One reference sits above the frontier depth, the other far below
			// it inside another branch; whichever the walk meets first claims
			// the subtree and the other must resolve to the same node.
			root := BeginCell().
				MustStoreRef(deep).
				MustStoreRef(BeginCell().MustStoreUInt(4, 64).MustStoreRef(shared).EndCell()).
				EndCell()
			rs := NewReadSet(root)
			readEverything(t, rs.Root())
			return rs
		}},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			rs := shape.build(t)
			sequential, err := rs.buildSourceGraph(1)
			if err != nil {
				t.Fatal(err)
			}
			ran := false
			for _, workers := range []int{2, 4, 16} {
				// Call the parallel builder directly, past the size gate: the
				// shapes here are chosen for their sharing structure, not
				// their bulk, and a silent decline would compare the
				// sequential walk with itself.
				parallel := newSourceGraphFor(rs)
				done, err := rs.buildSourceGraphParallel(parallel, workers)
				if err != nil {
					t.Fatalf("workers=%d: %v", workers, err)
				}
				if !done {
					if _, _, err := parallel.visit(rs, rs.source, 0); err != nil {
						t.Fatalf("workers=%d sequential fallback: %v", workers, err)
					}
				} else {
					ran = true
				}
				assertSameGraph(t, fmt.Sprintf("%s workers=%d", shape.name, workers), sequential, parallel)
			}
			if shape.mustParallelize && !ran {
				t.Fatal("the parallel build declined every worker count, the shape proves nothing")
			}
		})
	}
}

// TestParallelSourceGraphUpdateBytesMatch closes the loop where it matters: the
// merkle update built over the parallel graph must be byte-identical to the
// sequential one, at every worker count.
func TestParallelSourceGraphUpdateBytesMatch(t *testing.T) {
	from, keys := merkleUpdateSourceDict(t, 65536, 2026082020)
	makeUpdate := func(workers int) []byte {
		t.Helper()
		rs := NewReadSet(from)
		dict, err := rs.Root().BeginParse()
		if err != nil {
			t.Fatal(err)
		}
		rebuilt := dict.MustToCell().AsDict(64)
		rnd := rand.New(rand.NewSource(2026082021))
		for i := 0; i < 6000; i++ {
			if _, err := rebuilt.LoadValue(merkleUpdateDictKey(keys[rnd.Intn(len(keys))])); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 700; i++ {
			key := keys[rnd.Intn(len(keys))]
			if err := rebuilt.Set(merkleUpdateDictKey(key), merkleUpdateDictValue(key^uint64(i+7))); err != nil {
				t.Fatal(err)
			}
		}
		if workers > 1 && rs.Size() < sourceGraphParallelMinCells {
			t.Fatalf("recorded %d cells, under the parallel gate %d: the fixture proves nothing",
				rs.Size(), sourceGraphParallelMinCells)
		}
		update, _, _, err := rs.CreateMerkleUpdateAppliedSized(rebuilt.AsCell(), 0, workers)
		if err != nil {
			t.Fatal(err)
		}
		boc := update.ToBOC()
		return boc
	}

	reference := makeUpdate(1)
	for _, workers := range []int{2, 8, 16} {
		if got := makeUpdate(workers); !bytes.Equal(got, reference) {
			t.Fatalf("workers=%d: update bytes differ from the sequential build", workers)
		}
	}
}
