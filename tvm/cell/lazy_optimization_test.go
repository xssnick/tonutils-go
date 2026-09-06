package cell

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"unsafe"
)

func TestPrewarmRecursiveDeduplicatesLazyLoads(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).MustStoreRef(leaf).MustStoreRef(leaf).MustStoreRef(leaf).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{leaf.HashKey(): leaf}}
	lazy := cellWithLazyRefsFromCell(root, loader.LoadCell)

	got, err := lazy.PrewarmRecursive(0)
	if err != nil {
		t.Fatal(err)
	}
	if loader.calls != 1 {
		t.Fatalf("loader calls = %d, want 1", loader.calls)
	}
	if got.HashKey() != root.HashKey() {
		t.Fatal("prewarm changed the root hash")
	}
	for _, ref := range got.rawRefs() {
		if ref.IsLazy() || ref != got.refs[0] {
			t.Fatal("shared leaf was not materialized once")
		}
	}
	allocs := testing.AllocsPerRun(100, func() {
		benchmarkCellSink, err = lazy.PrewarmRecursive(0)
		if err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 2 {
		t.Fatalf("prewarm allocations = %v, want 2 materialized cells", allocs)
	}
}

func TestPrewarmRecursiveSharesBodiesAcrossDepthLimits(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	child := BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(leaf).EndCell()
	parent := BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(child).EndCell()
	root := BeginCell().MustStoreRef(child).MustStoreRef(parent).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{leaf.HashKey(): leaf}}
	loader.cells[child.HashKey()] = cellWithLazyRefsFromCell(child, loader.LoadCell)
	loader.cells[parent.HashKey()] = cellWithLazyRefsFromCell(parent, loader.LoadCell)
	lazy := cellWithLazyRefsFromCell(root, loader.LoadCell)

	got, err := lazy.PrewarmRecursive(2)
	if err != nil {
		t.Fatal(err)
	}
	if loader.calls != 3 {
		t.Fatalf("loader calls = %d, want 3", loader.calls)
	}
	if got.HashKey() != root.HashKey() {
		t.Fatal("prewarm changed the root hash")
	}
	if got.refs[0].refs[0].IsLazy() {
		t.Fatal("leaf inside depth limit stayed lazy")
	}
	if !got.refs[1].refs[0].refs[0].IsLazy() {
		t.Fatal("leaf beyond depth limit was materialized")
	}
}

func TestPrewarmRecursiveValidatesCachedLazyBoundary(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	for _, missingLoader := range []bool{false, true} {
		t.Run(fmt.Sprintf("missingLoader%v", missingLoader), func(t *testing.T) {
			loader := &testLazyLoader{cells: map[Hash]*Cell{leaf.HashKey(): leaf}}
			first := mustCreateLazyPrunedRef(t, lazyRefFromCell(leaf), loader.LoadCell)
			second := mustCreateLazyPrunedRef(t, lazyRefFromCell(leaf), loader.LoadCell)
			wantErr := ErrLazyRefMismatch
			if missingLoader {
				second.meta.lazyLoader = nil
				wantErr = ErrLazyLoaderNotSet
			} else {
				second.depth0++
			}
			root := BeginCell().MustStoreRef(first).MustStoreRef(second).EndCell()
			if _, err := root.PrewarmRecursive(0); !errors.Is(err, wantErr) {
				t.Fatalf("prewarm error = %v, want %v", err, wantErr)
			}
			if loader.calls != 1 {
				t.Fatalf("loader calls = %d, want 1", loader.calls)
			}
		})
	}
}

func TestLazyCachedLoadPreservesVirtualLevelsAndTraces(t *testing.T) {
	base := BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(BeginCell().EndCell()).EndCell()
	pruned, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	child := BeginCell().MustStoreRef(pruned).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{child.HashKey(): child}}
	boundary := mustCreateLazyPrunedRef(t, lazyRefFromCell(child), loader.LoadCell)
	var cache cellLoadCache
	for _, level := range []uint8{0, 1, 2} {
		trace := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
		view := boundary.Virtualize(level).WithTrace(trace)
		got, err := loadLazyPrunedRefCached(view, &cache)
		if err != nil {
			t.Fatal(err)
		}
		want := child.Virtualize(level)
		if got.HashKey() != want.HashKey() || got.Depth() != want.Depth() || got.Level() != want.Level() {
			t.Fatalf("cached load changed the view at level %d", level)
		}
		if got.Trace() != trace {
			t.Fatal("cached load reused another boundary's trace")
		}
	}
	if loader.calls != 1 {
		t.Fatalf("loader calls = %d, want 1", loader.calls)
	}
}

func TestBOCLazyMaterializationUsesOneAllocation(t *testing.T) {
	for refs := 0; refs <= 4; refs++ {
		t.Run(fmt.Sprintf("refs%d", refs), func(t *testing.T) {
			builder := BeginCell().MustStoreUInt(0xAA, 8)
			for i := 0; i < refs; i++ {
				builder.MustStoreRef(BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
			}
			child := builder.EndCell()
			root := BeginCell().MustStoreRef(child).EndCell()
			boc := root.ToBOCWithOptions(BOCSerializeOptions{WithIndex: true, WithCacheBits: true})
			parsed, err := FromBOCWithOptions(boc, BOCParseOptions{Lazy: true})
			if err != nil {
				t.Fatal(err)
			}
			boundary := parsed.MustPeekRef(0)
			allocs := testing.AllocsPerRun(100, func() {
				benchmarkCellSink, err = boundary.Prewarm()
				if err != nil {
					t.Fatal(err)
				}
			})
			if allocs != 1 {
				t.Fatalf("materialization allocations = %v, want 1", allocs)
			}
		})
	}
	if unsafe.Sizeof(uintptr(0)) == 8 && unsafe.Sizeof(cellMeta{}) != 40 {
		t.Fatalf("ordinary cellMeta grew: %d bytes", unsafe.Sizeof(cellMeta{}))
	}
}

func TestBOCLazyResolverSurvivesCopiesAndVirtualization(t *testing.T) {
	base := BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(BeginCell().EndCell()).EndCell()
	pruned, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	child := BeginCell().MustStoreRef(pruned).EndCell()
	root := BeginCell().MustStoreRef(child).EndCell()
	boc := root.ToBOCWithOptions(mode31Options())
	for _, trusted := range []bool{false, true} {
		t.Run(fmt.Sprintf("trusted%v", trusted), func(t *testing.T) {
			parsed, err := FromBOCWithOptions(boc, BOCParseOptions{
				Lazy: true, TrustedHashes: trusted, AllowNonZeroLevelRoot: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			boundary := parsed.MustPeekRef(0)
			trace := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
			variants := []*Cell{
				boundary, boundary.copy(), boundary.WithTrace(trace), boundary.WithTrace(trace).WithoutTrace(),
				boundary.Virtualize(0), boundary.Virtualize(1).WithTrace(trace),
				boundary.WithTrace(trace).Virtualize(0).copy(),
			}
			// Cloned prefix pointers must keep the full resolver extension alive.
			runtime.GC()
			for i, ref := range variants {
				got, err := ref.Prewarm()
				if err != nil {
					t.Fatalf("variant %d: %v", i, err)
				}
				want := child.Virtualize(uint8(ref.EffectiveLevel()))
				if got.HashKey() != want.HashKey() || got.Depth() != want.Depth() || got.Level() != want.Level() {
					t.Fatalf("variant %d changed the represented cell", i)
				}
				if got.Trace() != ref.Trace() {
					t.Fatalf("variant %d lost its trace", i)
				}
			}
		})
	}
}

func TestBOCLazyCacheAll(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{WithIndex: true, WithCacheBits: true})
	for _, tc := range []struct {
		name      string
		cacheAll  bool
		disable   bool
		wantCache bool
	}{
		{name: "cache bits"},
		{name: "all", cacheAll: true, wantCache: true},
		{name: "disabled", cacheAll: true, disable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := FromBOCWithOptions(boc, BOCParseOptions{
				Lazy: true, CacheAllLazyCells: tc.cacheAll, DisableLazyCache: tc.disable,
			})
			if err != nil {
				t.Fatal(err)
			}
			boundary := parsed.MustPeekRef(0)
			first, err := boundary.Prewarm()
			if err != nil {
				t.Fatal(err)
			}
			second, err := boundary.Prewarm()
			if err != nil {
				t.Fatal(err)
			}
			if (first == second) != tc.wantCache {
				t.Fatalf("same materialized pointer = %v, want %v", first == second, tc.wantCache)
			}
			allocs := testing.AllocsPerRun(100, func() {
				benchmarkCellSink, err = boundary.Prewarm()
				if err != nil {
					t.Fatal(err)
				}
			})
			wantAllocs := float64(1)
			if tc.wantCache {
				wantAllocs = 0
			}
			if allocs != wantAllocs {
				t.Fatalf("warm allocations = %v, want %v", allocs, wantAllocs)
			}
		})
	}
}

func TestBOCLazyCacheAllConcurrent(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{WithIndex: true, WithCacheBits: true})
	parsed, err := FromBOCWithOptions(boc, BOCParseOptions{Lazy: true, CacheAllLazyCells: true})
	if err != nil {
		t.Fatal(err)
	}
	boundary := parsed.MustPeekRef(0)
	var loaded [16]*Cell
	var errs [16]error
	var wg sync.WaitGroup
	for i := range loaded {
		wg.Go(func() {
			loaded[i], errs[i] = boundary.WithTrace(NewTrace(TraceHooks{OnLoad: func(*Cell) {}})).WithoutTrace().Prewarm()
		})
	}
	wg.Wait()
	for i := range loaded {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if loaded[i] != loaded[0] || loaded[i].HashKey() != leaf.HashKey() {
			t.Fatal("concurrent cache-all loads changed pointer identity or hash")
		}
	}
}

func TestPrewarmRecursiveDeduplicatesVirtualizedLazyLoads(t *testing.T) {
	base := BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(BeginCell().EndCell()).EndCell()
	pruned, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	child := BeginCell().MustStoreRef(pruned).EndCell()
	root := BeginCell().MustStoreRef(child).MustStoreRef(child).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{child.HashKey(): child}}
	lazy := cellWithLazyRefsFromCell(root, loader.LoadCell).Virtualize(0)

	got, err := lazy.PrewarmRecursive(1)
	if err != nil {
		t.Fatal(err)
	}
	if loader.calls != 1 {
		t.Fatalf("loader calls = %d, want 1", loader.calls)
	}
	want := root.Virtualize(0)
	if got.HashKey() != want.HashKey() || got.Depth() != want.Depth() || got.Level() != want.Level() {
		t.Fatal("prewarm changed the virtualized cell")
	}
	if got.IsVirtualized() || got.refs[0].IsLazy() || got.refs[0] != got.refs[1] {
		t.Fatal("virtualized shared child was not materialized once")
	}
}

func TestLazyCachedLoadSeparatesRawRepresentations(t *testing.T) {
	base := BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(BeginCell().EndCell()).EndCell()
	var cache cellLoadCache
	loader := &testLazyLoader{cells: map[Hash]*Cell{}}
	for _, level := range []uint8{1, 2} {
		pruned, err := createPrunedBranchFromCell(base, int(level))
		if err != nil {
			t.Fatal(err)
		}
		loader.cells[pruned.HashKey()] = pruned
		boundary := mustCreateLazyPrunedRef(t, lazyRefFromCell(pruned), loader.LoadCell).Virtualize(0)
		got, err := loadLazyPrunedRefCached(boundary, &cache)
		if err != nil {
			t.Fatal(err)
		}
		if got.HashKey() != base.HashKey() || got.rawCell().HashKey() != pruned.HashKey() {
			t.Fatal("equal visible hash reused a different raw representation")
		}
	}
	if loader.calls != 2 {
		t.Fatalf("loader calls = %d, want 2", loader.calls)
	}
}
