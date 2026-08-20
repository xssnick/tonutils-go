package cell

import (
	"errors"
	"strings"
	"testing"
)

type bulkLoadedPathRecorder struct {
	trace *Trace
	path  []*Cell
}

func newBulkLoadedPathRecorder() *bulkLoadedPathRecorder {
	recorder := &bulkLoadedPathRecorder{path: make([]*Cell, 0, 16)}
	recorder.trace = NewTraceForListener(recorder)
	return recorder
}

func (r *bulkLoadedPathRecorder) OnLoad(loaded *Cell) {
	r.path = append(r.path, loaded)
}

func (*bulkLoadedPathRecorder) OnCreate() {}

func (r *bulkLoadedPathRecorder) ChildTrace(int) *Trace {
	return r.trace
}

func (*bulkLoadedPathRecorder) PendingError() error {
	return nil
}

func TestSetManyWithLoadedPathsMatchesLazyAndEagerReferences(t *testing.T) {
	base := bulkLoadedDenseDict(t, 8, 64)
	entries := []AugmentedEntry{
		{Key: bulkLoadedKey(3, 8), Value: bulkValue(bulkLoadedKey(3, 8), 11), Mode: DictSetModeReplace},
		{Key: bulkLoadedKey(57, 8), Value: bulkValue(bulkLoadedKey(57, 8), 12), Mode: DictSetModeReplace},
		{Key: bulkLoadedKey(70, 8), Value: bulkValue(bulkLoadedKey(70, 8), 13), Mode: DictSetModeAdd},
		{Key: bulkLoadedKey(193, 8), Value: bulkValue(bulkLoadedKey(193, 8), 14), Mode: DictSetModeAdd},
	}

	eagerRepeated := base.Copy()
	for _, entry := range entries {
		if _, err := eagerRepeated.SetWithMode(entry.Key, entry.Value, entry.Mode); err != nil {
			t.Fatalf("eager repeated Set: %v", err)
		}
	}
	eagerBulk := base.Copy()
	if err := eagerBulk.SetMany(entries); err != nil {
		t.Fatalf("eager SetMany: %v", err)
	}

	lazyBulk, _, _ := bulkLoadedLazyDict(t, base, nil)
	if err := lazyBulk.SetMany(entries); err != nil {
		t.Fatalf("lazy SetMany: %v", err)
	}

	lazyLoaded, _, recorder := bulkLoadedLazyDict(t, base, nil)
	paths := make([][]*Cell, 0, len(entries))
	for _, entry := range entries {
		path, err := bulkLoadedLookupPath(lazyLoaded, recorder, entry.Key)
		if entry.Mode == DictSetModeReplace && err != nil {
			t.Fatalf("load replace path: %v", err)
		}
		if entry.Mode == DictSetModeAdd && !errors.Is(err, ErrNoSuchKeyInDict) {
			t.Fatalf("load insert path: got %v, want ErrNoSuchKeyInDict", err)
		}
		paths = append(paths, path)
	}
	if err := lazyLoaded.SetManyWithLoadedPaths(entries, paths); err != nil {
		t.Fatalf("SetManyWithLoadedPaths: %v", err)
	}

	wantRoot := eagerRepeated.RootCell().HashKey()
	wantWrapped := eagerRepeated.AsCell().HashKey()
	for _, result := range []struct {
		name string
		dict *AugmentedDictionary
	}{
		{name: "eager_bulk", dict: eagerBulk},
		{name: "lazy_bulk", dict: lazyBulk},
		{name: "lazy_loaded_paths", dict: lazyLoaded},
	} {
		t.Run(result.name, func(t *testing.T) {
			if got := result.dict.RootCell().HashKey(); got != wantRoot {
				t.Fatalf("root = %x, want %x", got, wantRoot)
			}
			if got := result.dict.AsCell().HashKey(); got != wantWrapped {
				t.Fatalf("wrapped root = %x, want %x", got, wantWrapped)
			}
		})
	}
}

func TestSetManyWithLoadedPathsLoadsOnlyUnseenBoundaryRoots(t *testing.T) {
	base := bulkLoadedDenseDict(t, 4, 16)
	entries := []AugmentedEntry{
		{Key: bulkLoadedKey(2, 4), Value: bulkValue(bulkLoadedKey(2, 4), 21), Mode: DictSetModeReplace},
		{Key: bulkLoadedKey(3, 4), Value: bulkValue(bulkLoadedKey(3, 4), 22), Mode: DictSetModeReplace},
	}

	t.Run("changed_paths_only", func(t *testing.T) {
		dict, loader, recorder := bulkLoadedLazyDict(t, base, nil)
		paths := bulkLoadedPaths(t, dict, recorder, []uint64{2, 3}, 4)
		before := bulkLoadedTotalCalls(loader.snapshot())

		if err := dict.SetManyWithLoadedPaths(entries, paths); err != nil {
			t.Fatalf("SetManyWithLoadedPaths: %v", err)
		}
		if got := bulkLoadedTotalCalls(loader.snapshot()) - before; got != 3 {
			t.Fatalf("mutation loader calls = %d, want 3 untouched boundary roots", got)
		}
	})

	t.Run("boundary_paths_already_loaded", func(t *testing.T) {
		dict, loader, recorder := bulkLoadedLazyDict(t, base, nil)
		paths := bulkLoadedPaths(t, dict, recorder, []uint64{2, 3, 0, 4, 8}, 4)
		before := bulkLoadedTotalCalls(loader.snapshot())

		if err := dict.SetManyWithLoadedPaths(entries, paths); err != nil {
			t.Fatalf("SetManyWithLoadedPaths: %v", err)
		}
		if got := bulkLoadedTotalCalls(loader.snapshot()) - before; got != 0 {
			t.Fatalf("mutation loader calls = %d, want 0 with the exact boundary roots resident", got)
		}
	})

	t.Run("ordinary_bulk_reloads_changed_spines", func(t *testing.T) {
		dict, loader, recorder := bulkLoadedLazyDict(t, base, nil)
		_ = bulkLoadedPaths(t, dict, recorder, []uint64{2, 3}, 4)
		before := bulkLoadedTotalCalls(loader.snapshot())

		if err := dict.SetMany(entries); err != nil {
			t.Fatalf("SetMany: %v", err)
		}
		if got := bulkLoadedTotalCalls(loader.snapshot()) - before; got <= 3 {
			t.Fatalf("ordinary bulk loader calls = %d, want more than the 3 boundary-only calls", got)
		}
	})
}

func TestSetManyWithLoadedPathsParallelMatchesSequentialAndDeduplicatesLoads(t *testing.T) {
	const keyBits = 10
	base := bulkLoadedDenseDict(t, keyBits, 512)
	entries := make([]AugmentedEntry, 0, 128)
	keys := make([]uint64, 0, cap(entries))
	for key := uint64(1); key < 512; key += 4 {
		keyCell := bulkLoadedKey(key, keyBits)
		keys = append(keys, key)
		entries = append(entries, AugmentedEntry{
			Key:   keyCell,
			Value: bulkValue(keyCell, key+1000),
			Mode:  DictSetModeReplace,
		})
	}

	sequential := base.Copy()
	if err := sequential.SetMany(entries); err != nil {
		t.Fatalf("sequential SetMany: %v", err)
	}

	loader := newPreparedLoader(base.RootCell())
	lazyWrapper := cellWithLazyRefsFromCell(base.AsCell(), loader.LoadCell)
	lazyRoot := lazyWrapper.MustPeekRef(0)
	readSet := NewReadSet(lazyRoot)
	parallel, _, recorder := bulkLoadedLazyDictWithRoot(base, lazyRoot, readSet.Trace(), loader)
	paths := bulkLoadedPaths(t, parallel, recorder, keys, keyBits)
	before := loader.snapshot()
	// Path collection is deliberately sequential. Once handed off, the
	// recorder is no longer part of the mutation trace; the production caller
	// follows the same ownership boundary before enabling branch parallelism.
	recorder.trace.DetachListener()
	if err := parallel.SetManyWithLoadedPaths(entries, paths, 8); err != nil {
		t.Fatalf("parallel SetManyWithLoadedPaths: %v", err)
	}
	if got, want := parallel.AsCell().HashKey(), sequential.AsCell().HashKey(); got != want {
		t.Fatalf("parallel root = %x, sequential = %x", got, want)
	}
	update, err := readSet.CreateMerkleUpdate(parallel.RootCell())
	if err != nil {
		t.Fatalf("CreateMerkleUpdate: %v", err)
	}
	if err = ValidateMerkleUpdate(update); err != nil {
		t.Fatalf("ValidateMerkleUpdate: %v", err)
	}
	applied, err := ApplyMerkleUpdate(base.RootCell(), update)
	if err != nil {
		t.Fatalf("ApplyMerkleUpdate: %v", err)
	}
	if got, want := applied.HashKey(), parallel.RootCell().HashKey(); got != want {
		t.Fatalf("applied root = %x, parallel = %x", got, want)
	}

	for hash, count := range loader.snapshot() {
		if delta := count - before[hash]; delta > 1 {
			t.Fatalf("boundary root %x loaded %d times during parallel mutation", hash, delta)
		}
	}
}

func TestSetManyWithLoadedPathsRejectsMissingChangedSpineWithoutFallback(t *testing.T) {
	base := bulkLoadedDenseDict(t, 4, 16)
	dict, loader, recorder := bulkLoadedLazyDict(t, base, nil)
	path := bulkLoadedPaths(t, dict, recorder, []uint64{2}, 4)[0]
	if len(path) < 2 {
		t.Fatalf("lookup path has %d cells, want at least 2", len(path))
	}

	rootBefore := dict.RootCell()
	wrapperBefore := dict.AsCell().HashKey()
	loadsBefore := bulkLoadedTotalCalls(loader.snapshot())
	entry := AugmentedEntry{
		Key:   bulkLoadedKey(2, 4),
		Value: bulkValue(bulkLoadedKey(2, 4), 31),
		Mode:  DictSetModeReplace,
	}
	err := dict.SetManyWithLoadedPaths([]AugmentedEntry{entry}, [][]*Cell{path[1:]})
	if err == nil || !strings.Contains(err.Error(), "loaded paths do not contain changed branch") {
		t.Fatalf("error = %v, want missing changed-branch rejection", err)
	}
	if got := bulkLoadedTotalCalls(loader.snapshot()); got != loadsBefore {
		t.Fatalf("loader calls after incomplete handoff = %d, want unchanged %d", got, loadsBefore)
	}
	if dict.RootCell() != rootBefore {
		t.Fatal("receiver root changed after rejected mutation")
	}
	if got := dict.AsCell().HashKey(); got != wrapperBefore {
		t.Fatalf("receiver wrapper = %x after error, want %x", got, wrapperBefore)
	}
}

func TestSetManyWithLoadedPathsRetainsReadSetMerkleUpdate(t *testing.T) {
	base := bulkLoadedDenseDict(t, 4, 16)
	entries := []AugmentedEntry{
		{Key: bulkLoadedKey(2, 4), Value: bulkValue(bulkLoadedKey(2, 4), 41), Mode: DictSetModeReplace},
		{Key: bulkLoadedKey(3, 4), Value: bulkValue(bulkLoadedKey(3, 4), 42), Mode: DictSetModeReplace},
	}

	build := func(t *testing.T, withLoadedPaths bool) (*Cell, *Cell, int) {
		t.Helper()

		loader := newPreparedLoader(base.RootCell())
		lazyWrapper := cellWithLazyRefsFromCell(base.AsCell(), loader.LoadCell)
		lazyRoot := lazyWrapper.MustPeekRef(0)
		readSet := NewReadSet(lazyRoot)
		dict, _, recorder := bulkLoadedLazyDictWithRoot(base, lazyRoot, readSet.Trace(), loader)
		paths := bulkLoadedPaths(t, dict, recorder, []uint64{2, 3}, 4)

		var err error
		if withLoadedPaths {
			err = dict.SetManyWithLoadedPaths(entries, paths)
		} else {
			err = dict.SetMany(entries)
		}
		if err != nil {
			t.Fatalf("apply mutation: %v", err)
		}

		readCells := readSet.Size()
		update, err := readSet.CreateMerkleUpdate(dict.RootCell())
		if err != nil {
			t.Fatalf("CreateMerkleUpdate: %v", err)
		}
		if err = ValidateMerkleUpdate(update); err != nil {
			t.Fatalf("ValidateMerkleUpdate: %v", err)
		}
		applied, err := ApplyMerkleUpdate(base.RootCell(), update)
		if err != nil {
			t.Fatalf("ApplyMerkleUpdate: %v", err)
		}
		if applied.HashKey() != dict.RootCell().HashKey() {
			t.Fatal("Merkle update rebuilt a different destination")
		}
		return dict.RootCell(), update, readCells
	}

	ordinaryRoot, ordinaryUpdate, ordinaryReads := build(t, false)
	loadedRoot, loadedUpdate, loadedReads := build(t, true)
	if loadedRoot.HashKey() != ordinaryRoot.HashKey() {
		t.Fatal("loaded-path and ordinary bulk roots differ")
	}
	if loadedUpdate.HashKey() != ordinaryUpdate.HashKey() {
		t.Fatalf("loaded-path Merkle update = %x, ordinary = %x", loadedUpdate.Hash()[:8], ordinaryUpdate.Hash()[:8])
	}
	if loadedReads != ordinaryReads {
		t.Fatalf("loaded-path read set has %d cells, ordinary has %d", loadedReads, ordinaryReads)
	}
}

func bulkLoadedDenseDict(tb testing.TB, keyBits uint, count int) *AugmentedDictionary {
	tb.Helper()

	dict, err := NewAugDict(keyBits, bulkSumAugmentation{})
	if err != nil {
		tb.Fatal(err)
	}
	for i := 0; i < count; i++ {
		key := bulkLoadedKey(uint64(i), keyBits)
		if err = dict.Set(key, bulkValue(key, 0)); err != nil {
			tb.Fatalf("seed key %d: %v", i, err)
		}
	}
	return dict
}

func bulkLoadedKey(key uint64, keyBits uint) *Cell {
	return BeginCell().MustStoreUInt(key, keyBits).EndCell()
}

func bulkLoadedLazyDict(
	tb testing.TB,
	base *AugmentedDictionary,
	extraTrace *Trace,
) (*AugmentedDictionary, *preparedLoader, *bulkLoadedPathRecorder) {
	tb.Helper()

	loader := newPreparedLoader(base.RootCell())
	lazyWrapper := cellWithLazyRefsFromCell(base.AsCell(), loader.LoadCell)
	lazyRoot := lazyWrapper.MustPeekRef(0)
	return bulkLoadedLazyDictWithRoot(base, lazyRoot, extraTrace, loader)
}

func bulkLoadedLazyDictWithRoot(
	base *AugmentedDictionary,
	lazyRoot *Cell,
	extraTrace *Trace,
	loader *preparedLoader,
) (*AugmentedDictionary, *preparedLoader, *bulkLoadedPathRecorder) {
	recorder := newBulkLoadedPathRecorder()
	trace := CombineTraces(extraTrace, recorder.trace)
	dict := base.Copy()
	dict.root = lazyRoot
	dict.trace = nil
	dict.SetTrace(trace)
	return dict, loader, recorder
}

func bulkLoadedLookupPath(
	dict *AugmentedDictionary,
	recorder *bulkLoadedPathRecorder,
	key *Cell,
) ([]*Cell, error) {
	recorder.path = recorder.path[:0]
	_, err := dict.LoadValue(key)
	return append([]*Cell(nil), recorder.path...), err
}

func bulkLoadedPaths(
	tb testing.TB,
	dict *AugmentedDictionary,
	recorder *bulkLoadedPathRecorder,
	keys []uint64,
	keyBits uint,
) [][]*Cell {
	tb.Helper()

	paths := make([][]*Cell, 0, len(keys))
	for _, key := range keys {
		path, err := bulkLoadedLookupPath(dict, recorder, bulkLoadedKey(key, keyBits))
		if err != nil {
			tb.Fatalf("load path for key %d: %v", key, err)
		}
		paths = append(paths, path)
	}
	return paths
}

func bulkLoadedTotalCalls(calls map[Hash]int) int {
	total := 0
	for _, count := range calls {
		total += count
	}
	return total
}
