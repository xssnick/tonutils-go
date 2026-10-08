package cell

import (
	"bytes"
	"errors"
	"runtime"
	"testing"
)

var withTraceRetagSink *Cell

func TestWithTraceRetagPreservesCellAndMetadata(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	base := BeginCell().MustStoreUInt(0xCD, 8).MustStoreRef(leaf).EndCell()
	pruned, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	multilevel := BeginCell().MustStoreRef(pruned).EndCell()
	if multilevel.Level() != 2 || multilevel.meta.extraHashes == nil {
		t.Fatal("fixture must contain nonzero-level hashes")
	}
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(multilevel), func(Hash) (*Cell, error) {
		return multilevel, nil
	})
	parsed, err := FromBOCWithOptions(BeginCell().MustStoreRef(multilevel).EndCell().ToBOC(), BOCParseOptions{
		Lazy: true, AllowNonZeroLevelRoot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	bocLazy := parsed.MustPeekRef(0)
	for _, tc := range []struct {
		name string
		cell *Cell
	}{
		{"ordinary", base},
		{"multilevel", multilevel},
		{"virtual", multilevel.Virtualize(0)},
		{"lazy", lazy},
		{"lazy_virtual", lazy.Virtualize(1)},
		{"boc_lazy", bocLazy},
		{"boc_lazy_virtual", bocLazy.Virtualize(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var oldLoads, newLoads int
			oldTrace := NewTrace(TraceHooks{OnLoad: func(*Cell) { oldLoads++ }})
			newTrace := NewTrace(TraceHooks{OnLoad: func(*Cell) { newLoads++ }})
			original := tc.cell.WithTrace(oldTrace)
			replaced := original.WithTrace(newTrace)
			removed := replaced.WithoutTrace()
			if original.Trace() != oldTrace || replaced.Trace() != newTrace || removed.Trace() != nil {
				t.Fatal("retagging changed another wrapper's trace")
			}
			if original.WithTrace(oldTrace) != original || removed.WithoutTrace() != removed {
				t.Fatal("unchanged traces must preserve wrapper identity")
			}
			if oldLoads != 0 || newLoads != 0 {
				t.Fatal("retagging emitted a load event")
			}
			// A tagged BoC metadata pointer must retain its resolver allocation.
			runtime.GC()
			for _, got := range []*Cell{original, replaced, removed} {
				if got.IsLazy() != tc.cell.IsLazy() || got.IsVirtualized() != tc.cell.IsVirtualized() ||
					got.LevelMask() != tc.cell.LevelMask() || got.EffectiveLevel() != tc.cell.EffectiveLevel() ||
					got.ActualLevel() != tc.cell.ActualLevel() || got.bitsSz != tc.cell.bitsSz ||
					!bytes.Equal(got.data, tc.cell.data) || got.refs != tc.cell.refs {
					t.Fatal("retagging changed cell contents or view")
				}
				for level := 0; level <= _DataCellMaxLevel; level++ {
					if !bytes.Equal(got.Hash(level), tc.cell.Hash(level)) || got.Depth(level) != tc.cell.Depth(level) {
						t.Fatalf("retagging changed hash or depth at level %d", level)
					}
				}
				if tc.cell.meta != nil && tc.cell.meta.extraHashes != nil {
					if got.meta == tc.cell.meta || got.meta.extraHashes == tc.cell.meta.extraHashes ||
						*got.meta.extraHashes != *tc.cell.meta.extraHashes {
						t.Fatal("retagging must keep an independent copy of the extra hashes")
					}
				}
				loaded, err := got.Prewarm()
				if err != nil {
					t.Fatal(err)
				}
				if loaded.IsLazy() || loaded.HashKey() != got.HashKey() || loaded.Trace() != got.Trace() {
					t.Fatal("retagging changed materialization")
				}
			}
			if _, err := replaced.BeginParse(); err != nil {
				t.Fatal(err)
			}
			if oldLoads != 0 || newLoads != 1 {
				t.Fatalf("parse events old=%d new=%d, want 0 and 1", oldLoads, newLoads)
			}
		})
	}
}

func TestWithTraceRetagAllocations(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(7, 8).EndCell()
	first := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	second := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	traced := leaf.WithTrace(first)
	for _, trace := range []*Trace{second, nil} {
		if allocations := testing.AllocsPerRun(100, func() {
			withTraceRetagSink = traced.WithTrace(trace)
		}); allocations != 1 {
			t.Fatalf("replacing/removing an ordinary trace allocated %v times, want 1", allocations)
		}
	}
}

func TestWithTraceRetagPreservesErrors(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(7, 8).EndCell()
	want := errors.New("trace load failed")
	trace := NewTrace(TraceHooks{OnLoad: func(*Cell) {}, PendingError: func() error { return want }})
	oldTrace := NewTrace(TraceHooks{OnLoad: func(*Cell) { t.Fatal("replaced trace was invoked") }})
	if _, err := leaf.WithTrace(oldTrace).WithTrace(trace).BeginParse(); !errors.Is(err, want) {
		t.Fatalf("trace error: got %v, want %v", err, want)
	}
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(leaf))
	for _, c := range []*Cell{lazy.WithTrace(oldTrace).WithTrace(trace), lazy.WithTrace(oldTrace).WithoutTrace()} {
		if _, err := c.Prewarm(); !errors.Is(err, ErrLazyLoaderNotSet) {
			t.Fatalf("missing loader error: got %v, want ErrLazyLoaderNotSet", err)
		}
	}
}
