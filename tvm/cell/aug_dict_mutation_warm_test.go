package cell

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// TestReplayReusesTheMutationsWarmSiblings pins both halves of the warm-cell
// handoff: the replay must stop asking storage for siblings the mutation
// already resolved, and it must record exactly the reads it recorded before,
// because the collated proof is selected from that read set.
func TestReplayReusesTheMutationsWarmSiblings(t *testing.T) {
	for _, mode := range []struct {
		name   string
		replay func(*AugmentedDictionaryDiff) error
	}{
		{"sequential", (*AugmentedDictionaryDiff).Replay},
		{"parallel", func(d *AugmentedDictionaryDiff) error { return d.ReplayParallel(8) }},
	} {
		t.Run(mode.name, func(t *testing.T) {
			warmReads, warmProof, warmRoot, warmLoads := warmReplayRun(t, mode.replay, true)
			coldReads, coldProof, coldRoot, coldLoads := warmReplayRun(t, mode.replay, false)

			if coldLoads == 0 {
				t.Fatal("fixture never made the replay load a sibling, so it cannot prove the handoff removes one")
			}
			t.Logf("cold replay loads=%d warm=%d", coldLoads, warmLoads)
			if warmLoads != 0 {
				t.Fatalf("replay loaded %d sibling(s) the mutation had already resolved, cold replay loads %d", warmLoads, coldLoads)
			}
			if warmRoot != coldRoot {
				t.Fatal("warm replay changed the resulting dictionary")
			}
			if warmProof.HashKey() != coldProof.HashKey() {
				t.Fatalf("warm proof = %x, cold proof = %x", warmProof.Hash(), coldProof.Hash())
			}
			assertSourceReadSetsEqual(t, coldReads.Root(), coldReads, warmReads)
		})
	}
}

// warmReplayRun performs one bulk mutation over a lazy dictionary and replays
// its receipt, either with the warm cells the mutation collected or with them
// dropped. It returns the recorded reads, the proof selected from them, the
// resulting root and the number of storage loads the replay itself caused.
func warmReplayRun(
	t *testing.T,
	replay func(*AugmentedDictionaryDiff) error,
	warm bool,
) (*ReadSet, *Cell, Hash, int) {
	t.Helper()

	const keyBits = 8
	base := bulkLoadedDenseDict(t, keyBits, 128)
	entries := make([]AugmentedEntry, 0, 32)
	for i := 0; i < 16; i++ {
		key := bulkLoadedKey(uint64(i*7), keyBits)
		entries = append(entries, AugmentedEntry{Key: key, Value: bulkValue(key, uint64(100+i)), Mode: DictSetModeReplace})
	}
	for i := 0; i < 16; i++ {
		key := bulkLoadedKey(uint64(160+i), keyBits)
		entries = append(entries, AugmentedEntry{Key: key, Value: bulkValue(key, uint64(200+i)), Mode: DictSetModeAdd})
	}

	read := NewReadSet(base.RootCell())
	dict, loader, recorder := bulkLoadedLazyDict(t, base, read.Trace())
	paths := make([][]*Cell, 0, len(entries))
	for _, entry := range entries {
		path, err := bulkLoadedLookupPath(dict, recorder, entry.Key)
		if entry.Mode == DictSetModeReplace {
			if err != nil {
				t.Fatalf("load replace path: %v", err)
			}
		} else if !errors.Is(err, ErrNoSuchKeyInDict) {
			t.Fatalf("load insert path: got %v, want ErrNoSuchKeyInDict", err)
		}
		paths = append(paths, path)
	}
	recorder.trace.DetachListener()

	diff, err := dict.SetManyWithLoadedPathsAndDiff(entries, paths, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.warm) == 0 {
		t.Fatal("mutation kept no warm cells, so the run cannot tell the two paths apart")
	}
	if !warm {
		diff.warm = nil
	}

	before := totalPreparedLoads(loader)
	if err = replay(diff); err != nil {
		t.Fatal(err)
	}
	loads := totalPreparedLoads(loader) - before

	proof, err := read.Proof()
	if err != nil {
		t.Fatal(err)
	}
	return read, proof, dict.RootCell().HashKey(), loads
}

// totalPreparedLoads counts storage trips rather than distinct cells: the
// handoff removes trips for cells the mutation had already fetched, which
// loadCount, being a set size, cannot see.
func totalPreparedLoads(l *preparedLoader) int {
	total := 0
	for _, calls := range l.snapshot() {
		total += calls
	}
	return total
}

// TestWarmChildKeepsTheTraceOfTheLazyLoad pins the invariant the end-to-end
// replay cannot see: a substituted child must carry the very trace the lazy
// load would have carried. The mutation happens to record every kept sibling
// itself, so a replay that dropped the trace still produces the same proof
// today — and would silently stop doing so the moment a caller replays a
// closure the mutation did not walk.
func TestWarmChildKeepsTheTraceOfTheLazyLoad(t *testing.T) {
	const keyBits = 8
	base := bulkLoadedDenseDict(t, keyBits, 128)
	read := NewReadSet(base.RootCell())
	dict, loader, _ := bulkLoadedLazyDict(t, base, read.Trace())
	// The fixture's recorder combines into a pair trace, whose Child builds a
	// fresh trace per call; the listener trace alone returns a stable one, so
	// the two children can be compared at all.
	dict.trace = nil
	dict.SetTrace(read.Trace())

	node, err := parseFixedDictNodeWithTrace(dict.root, keyBits, dict.trace)
	if err != nil {
		t.Fatal(err)
	}

	var cold augmentedNodeChecker
	lazy, err := cold.child(node, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !lazy.IsLazy() {
		t.Fatal("fixture child is resident, so the substitution is never exercised")
	}
	if lazy.Trace() == nil {
		t.Fatal("fixture child carries no trace, so losing it would be invisible")
	}

	resolved, err := loader.LoadCell(lazy.rawCell().HashKey())
	if err != nil {
		t.Fatal(err)
	}
	warm := augmentedNodeChecker{warm: map[Hash]*Cell{lazy.rawCell().HashKey(): resolved.rawCell()}}
	got, err := warm.child(node, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.IsLazy() {
		t.Fatal("warm child was not substituted")
	}
	if got.HashKey() != lazy.HashKey() {
		t.Fatalf("warm child = %x, lazy child = %x", got.Hash()[:4], lazy.Hash()[:4])
	}
	if got.Trace() != lazy.Trace() {
		t.Fatal("warm child lost the trace the lazy load would have carried")
	}
}

// deepLazyStore is a store whose every answer is itself lazy one level down,
// the way a cell database behaves. preparedLoader hands back the original
// resident subtree instead, so a single load there makes everything under it
// free and no sibling below the top is ever fetched twice.
type deepLazyStore struct {
	mu    sync.Mutex
	cells map[Hash]*Cell
	loads int
}

func newDeepLazyStore(root *Cell) *deepLazyStore {
	s := &deepLazyStore{cells: map[Hash]*Cell{}}
	var index func(*Cell)
	index = func(c *Cell) {
		if c == nil {
			return
		}
		key := c.HashKey()
		if _, seen := s.cells[key]; seen {
			return
		}
		s.cells[key] = c
		for _, ref := range c.rawRefs() {
			index(ref)
		}
	}
	index(root)
	return s
}

func (s *deepLazyStore) load(h Hash) (*Cell, error) {
	s.mu.Lock()
	s.loads++
	c := s.cells[h]
	s.mu.Unlock()
	if c == nil {
		return nil, ErrLazyRefNotFound
	}
	return cellWithLazyRefsFromCell(c, s.load), nil
}

func (s *deepLazyStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads
}

// BenchmarkReplayWarmSiblings reports what the handoff removes on an
// account-shaped dictionary: 256-bit keys, a state of fifty thousand entries
// and a block-sized batch of changes, over a store that stays lazy all the way
// down. The wall clock still understates the win by whatever a real store read
// costs above a map lookup; the load count is the honest figure.
func BenchmarkReplayWarmSiblings(b *testing.B) {
	for _, shape := range []struct{ size, changed int }{{5_000, 400}, {50_000, 400}, {200_000, 1800}} {
		b.Run(fmt.Sprintf("state%d_changed%d", shape.size, shape.changed), func(b *testing.B) {
			benchReplayWarm(b, shape.size, shape.changed)
		})
	}
}

func benchReplayWarm(b *testing.B, size, changedCount int) {
	const keyBits = 256
	base, err := NewAugDict(keyBits, bulkSumAugmentation{})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < size; i++ {
		key := bulkLoadedKey(uint64(i)*2654435761, keyBits)
		if err = base.Set(key, bulkValue(key, 0)); err != nil {
			b.Fatal(err)
		}
	}
	entries := make([]AugmentedEntry, 0, changedCount)
	for i := 0; i < changedCount; i++ {
		key := bulkLoadedKey(uint64(i)*2654435761, keyBits)
		entries = append(entries, AugmentedEntry{Key: key, Value: bulkValue(key, uint64(i+1)), Mode: DictSetModeReplace})
	}

	for _, warm := range []bool{false, true} {
		name := "cold"
		if warm {
			name = "warm"
		}
		b.Run(name, func(b *testing.B) {
			loads := 0
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				store := newDeepLazyStore(base.RootCell())
				read := NewReadSet(base.RootCell())
				recorder := newBulkLoadedPathRecorder()
				lazyRoot, err := store.load(base.RootCell().HashKey())
				if err != nil {
					b.Fatal(err)
				}
				dict := base.Copy()
				dict.root = lazyRoot
				dict.trace = nil
				dict.SetTrace(CombineTraces(read.Trace(), recorder.trace))
				paths := make([][]*Cell, 0, len(entries))
				for _, entry := range entries {
					path, lookupErr := bulkLoadedLookupPath(dict, recorder, entry.Key)
					if lookupErr != nil {
						b.Fatal(lookupErr)
					}
					paths = append(paths, path)
				}
				recorder.trace.DetachListener()
				diff, err := dict.SetManyWithLoadedPathsAndDiff(entries, paths, 8)
				if err != nil {
					b.Fatal(err)
				}
				if !warm {
					diff.warm = nil
				}
				before := store.count()
				b.StartTimer()
				if err = diff.ReplayParallel(8); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				loads += store.count() - before
				b.StartTimer()
			}
			b.ReportMetric(float64(loads)/float64(b.N), "loads/op")

		})
	}
}
