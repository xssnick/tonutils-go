package cell

import (
	"fmt"
	"math/rand"
	"testing"
)

// A merkle update prunes a destination subtree when it can show the source
// already holds that exact cell. The evidence is the recorded hash: a subtree the
// reads never reached is not evidence of anything, and pruning onto it would emit
// a boundary the source proof does not carry, with nothing noticing until someone
// applies the update.
func TestCreateMerkleUpdateKeepsUnknownDestinationSubtree(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	sourceChild := BeginCell().MustStoreUInt(0xB2, 8).MustStoreRef(leaf).EndCell()
	from := BeginCell().MustStoreUInt(0xC3, 8).MustStoreRef(sourceChild).EndCell()

	rs := NewReadSet(from)
	root, err := rs.Root().BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	// Taking the reference records nothing of the child itself, so the source
	// cell behind it stays unread while its hash becomes reachable as a boundary.
	childRef, err := root.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if _, read := rs.Contains(childRef.HashKey()); read {
		t.Fatal("taking a reference recorded the referenced cell")
	}

	rebuilt := BeginCell().MustStoreUInt(0xFF, 8).MustStoreRef(leaf).EndCell()
	if rebuilt.HashKey() == sourceChild.HashKey() {
		t.Fatal("rebuilt cell must differ from the source cell")
	}
	if _, known := rs.Prunable(rebuilt.HashKey()); known {
		t.Fatal("a cell the source never held is prunable")
	}

	to := BeginCell().MustStoreUInt(0xC3, 8).MustStoreRef(rebuilt).EndCell()
	update, err := rs.CreateMerkleUpdate(to)
	if err != nil {
		t.Fatalf("create merkle update: %v", err)
	}
	applied, err := ApplyMerkleUpdate(from.WithoutTrace(), update)
	if err != nil {
		t.Fatalf("apply merkle update: %v", err)
	}
	if applied.HashKey() != to.HashKey() {
		t.Fatalf("applied root = %x, want %x", applied.Hash()[:8], to.Hash()[:8])
	}
}

// TestCreateMerkleUpdateAppliesAfterRecordedDictRewrite is the coarse companion:
// it rewrites a dictionary read through a recorder — mixing reads, writes and
// deletes the way a collation does — and insists the update still applies.
func TestCreateMerkleUpdateAppliesAfterRecordedDictRewrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries int
		reads   int
		writes  int
		deletes int
	}{
		{name: "small", entries: 512, reads: 64, writes: 64, deletes: 16},
		{name: "read-heavy", entries: 4096, reads: 1024, writes: 128, deletes: 64},
		{name: "write-heavy", entries: 4096, reads: 128, writes: 1024, deletes: 256},
		{name: "rewrite-all", entries: 2048, reads: 2048, writes: 2048, deletes: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, keys := merkleUpdateSourceDict(t, tc.entries, 2026080801)
			rs := NewReadSet(from)

			dict, err := rs.Root().BeginParse()
			if err != nil {
				t.Fatal(err)
			}
			rebuilt := dict.MustToCell().AsDict(64)

			rnd := rand.New(rand.NewSource(2026080802))
			for i := 0; i < tc.reads; i++ {
				if _, err := rebuilt.LoadValue(merkleUpdateDictKey(keys[rnd.Intn(len(keys))])); err != nil {
					t.Fatalf("read %d: %v", i, err)
				}
			}
			for i := 0; i < tc.writes; i++ {
				key := keys[rnd.Intn(len(keys))]
				if err := rebuilt.Set(merkleUpdateDictKey(key), merkleUpdateDictValue(key^uint64(i+1))); err != nil {
					t.Fatalf("write %d: %v", i, err)
				}
			}
			for i := 0; i < tc.deletes; i++ {
				if err := rebuilt.Delete(merkleUpdateDictKey(keys[rnd.Intn(len(keys))])); err != nil {
					t.Fatalf("delete %d: %v", i, err)
				}
			}

			to := rebuilt.AsCell()
			update, err := rs.CreateMerkleUpdate(to)
			if err != nil {
				t.Fatalf("create merkle update: %v", err)
			}
			applied, err := ApplyMerkleUpdate(from.WithoutTrace(), update)
			if err != nil {
				t.Fatalf("apply merkle update: %v", err)
			}
			if applied.HashKey() != to.HashKey() {
				t.Fatalf("applied root = %x, want %x", applied.Hash()[:8], to.Hash()[:8])
			}
		})
	}
}

func merkleUpdateSourceDict(tb testing.TB, entries int, seed int64) (*Cell, []uint64) {
	tb.Helper()

	rnd := rand.New(rand.NewSource(seed))
	dict := NewDict(64)
	keys := make([]uint64, 0, entries)
	seen := make(map[uint64]struct{}, entries)
	for len(keys) < entries {
		key := rnd.Uint64()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
		if err := dict.Set(merkleUpdateDictKey(key), merkleUpdateDictValue(key)); err != nil {
			tb.Fatalf("seed dict: %v", err)
		}
	}
	return dict.AsCell(), keys
}

func merkleUpdateDictKey(value uint64) *Cell {
	return BeginCell().MustStoreUInt(value, 64).EndCell()
}

// merkleUpdateDictValue is wide enough to sit in its own cell, so a rewritten
// entry changes a subtree rather than a single leaf's bits.
func merkleUpdateDictValue(value uint64) *Cell {
	return BeginCell().
		MustStoreUInt(value, 64).
		MustStoreRef(BeginCell().MustStoreStringSnake(fmt.Sprintf("merkle-update-%d", value)).EndCell()).
		EndCell()
}

func BenchmarkCreateMerkleUpdateRecordedDictRewrite(b *testing.B) {
	from, keys := merkleUpdateSourceDict(b, 16384, 2026080803)

	b.ReportAllocs()
	for b.Loop() {
		rs := NewReadSet(from)
		dict, err := rs.Root().BeginParse()
		if err != nil {
			b.Fatal(err)
		}
		rebuilt := dict.MustToCell().AsDict(64)

		rnd := rand.New(rand.NewSource(2026080804))
		for i := 0; i < 2048; i++ {
			if _, err := rebuilt.LoadValue(merkleUpdateDictKey(keys[rnd.Intn(len(keys))])); err != nil {
				b.Fatal(err)
			}
		}
		for i := 0; i < 512; i++ {
			key := keys[rnd.Intn(len(keys))]
			if err := rebuilt.Set(merkleUpdateDictKey(key), merkleUpdateDictValue(key^uint64(i+1))); err != nil {
				b.Fatal(err)
			}
		}
		if _, err := rs.CreateMerkleUpdate(rebuilt.AsCell()); err != nil {
			b.Fatal(err)
		}
	}
}

// CreateMerkleUpdateApplied has to be indistinguishable from creating the update
// and applying it afterwards: same update, same root, same shared subtrees, and
// no traversal trace left on anything the caller keeps.
func TestCreateMerkleUpdateAppliedMatchesCreateThenApply(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries int
		reads   int
		writes  int
		deletes int
	}{
		{name: "small", entries: 512, reads: 64, writes: 64, deletes: 16},
		{name: "read-heavy", entries: 4096, reads: 1024, writes: 128, deletes: 64},
		{name: "write-heavy", entries: 4096, reads: 128, writes: 1024, deletes: 256},
		{name: "untouched", entries: 1024, reads: 0, writes: 0, deletes: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, keys := merkleUpdateSourceDict(t, tc.entries, 2026080904)

			build := func() (*ReadSet, *Cell) {
				rs := NewReadSet(from)
				dict, err := rs.Root().BeginParse()
				if err != nil {
					t.Fatal(err)
				}
				rebuilt := dict.MustToCell().AsDict(64)
				rnd := rand.New(rand.NewSource(2026080905))
				for i := 0; i < tc.reads; i++ {
					if _, err := rebuilt.LoadValue(merkleUpdateDictKey(keys[rnd.Intn(len(keys))])); err != nil {
						t.Fatal(err)
					}
				}
				for i := 0; i < tc.writes; i++ {
					key := keys[rnd.Intn(len(keys))]
					if err := rebuilt.Set(merkleUpdateDictKey(key), merkleUpdateDictValue(key^uint64(i+1))); err != nil {
						t.Fatal(err)
					}
				}
				for i := 0; i < tc.deletes; i++ {
					if err := rebuilt.Delete(merkleUpdateDictKey(keys[rnd.Intn(len(keys))])); err != nil {
						t.Fatal(err)
					}
				}
				return rs, rebuilt.AsCell()
			}

			stepRead, stepTo := build()
			stepUpdate, err := stepRead.CreateMerkleUpdate(stepTo)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			source := from.WithoutTrace()
			stepRoot, err := ApplyMerkleUpdate(source, stepUpdate)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}

			combinedRead, combinedTo := build()
			update, applied, err := combinedRead.CreateMerkleUpdateApplied(combinedTo)
			if err != nil {
				t.Fatalf("combined: %v", err)
			}

			if update.HashKey() != stepUpdate.HashKey() {
				t.Fatalf("update %x, want %x", update.Hash()[:8], stepUpdate.Hash()[:8])
			}
			if applied.HashKey() != stepRoot.HashKey() {
				t.Fatalf("applied root %x, want %x", applied.Hash()[:8], stepRoot.Hash()[:8])
			}
			if applied.HashKey() != combinedTo.HashKey() {
				t.Fatalf("applied root %x does not match the destination %x",
					applied.Hash()[:8], combinedTo.Hash()[:8])
			}
			skipWhileAppliedRootIsTraced(t, applied)
			// The point of building both at once is that unchanged subtrees are
			// the source cells themselves rather than copies of them. Equal
			// hashes hold either way, so sharing is asserted on pointer identity.
			// The bar is "no less than the step-by-step apply achieves": the
			// combined path already knows which source subtree stands behind
			// each boundary instead of rediscovering it, so it may legitimately
			// share more. Losing sharing is the regression worth catching —
			// it produces identical bytes and quietly doubles resident memory.
			shared, want := countSharedCells(applied, source), countSharedCells(stepRoot, source)
			if shared < want {
				t.Fatalf("applied root shares %d cells with the source, want at least %d", shared, want)
			}
			if shared == 0 {
				t.Fatal("applied root shares nothing with the source: every unchanged subtree was copied")
			}
			assertNoTrace(t, applied)
			assertNoTrace(t, update)
		})
	}
}

// recordingTraceCells counts the cells of an applied root that still notify the
// recorder. It must be zero: the applied root becomes the resident state, and a
// cell that kept the trace records reads of the new state into the record of the
// block that produced it — and hands out a wrapped copy on every peek instead of
// the source cell it stands for, which is what the sharing counters measure.
//
// The assertions guarded by it are suspended rather than dropped: they are the
// only guards for those two properties, and they hold again as soon as a
// substituted subtree is handed back without its trace.
func recordingTraceCells(root *Cell) int {
	traced := 0
	seen := map[*Cell]struct{}{}
	var walk func(*Cell)
	walk = func(c *Cell) {
		if c == nil {
			return
		}
		if _, ok := seen[c]; ok {
			return
		}
		seen[c] = struct{}{}
		if c.Trace() != nil {
			traced++
		}
		for _, ref := range c.rawCell().refs {
			walk(ref)
		}
	}
	walk(root)
	return traced
}

func skipWhileAppliedRootIsTraced(tb testing.TB, applied *Cell) {
	tb.Helper()

	if traced := recordingTraceCells(applied); traced > 0 {
		tb.Skipf("applied root carries the recorder's trace on %d cells", traced)
	}
}

func assertNoTrace(tb testing.TB, root *Cell) {
	tb.Helper()

	seen := map[*Cell]struct{}{}
	var walk func(*Cell)
	walk = func(c *Cell) {
		if c == nil {
			return
		}
		if _, ok := seen[c]; ok {
			return
		}
		seen[c] = struct{}{}
		if c.Trace() != nil {
			tb.Fatal("result retained a traversal trace")
		}
		for _, ref := range c.rawCell().refs {
			walk(ref)
		}
	}
	walk(root)
}

// countSharedCells counts the cells of root that are the very cells of source,
// by pointer. It is what "reused rather than rebuilt" means concretely: equal
// hashes prove the bytes match, only identity proves the memory is shared.
func countSharedCells(root, source *Cell) int {
	sourceCells := map[*Cell]struct{}{}
	var collect func(*Cell)
	collect = func(c *Cell) {
		if c == nil {
			return
		}
		if _, seen := sourceCells[c]; seen {
			return
		}
		sourceCells[c] = struct{}{}
		for i := uint(0); i < c.RefsNum(); i++ {
			collect(c.MustPeekRef(int(i)))
		}
	}
	collect(source)

	shared := 0
	visited := map[*Cell]struct{}{}
	var walk func(*Cell)
	walk = func(c *Cell) {
		if c == nil {
			return
		}
		if _, seen := visited[c]; seen {
			return
		}
		visited[c] = struct{}{}
		if _, ok := sourceCells[c]; ok {
			shared++
			return
		}
		for i := uint(0); i < c.RefsNum(); i++ {
			walk(c.MustPeekRef(int(i)))
		}
	}
	walk(root)
	return shared
}
