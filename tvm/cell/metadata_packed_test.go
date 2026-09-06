package cell

import (
	"reflect"
	"testing"
)

// collectMetadataHashesDepths keeps the independently allocated representation
// used by storage test fixtures and by the metadata reference implementation.
func collectMetadataHashesDepths(c *Cell, mask LevelMask) ([]Hash, []uint16) {
	hashes := make([]Hash, mask.getHashesCount())
	depths := make([]uint16, len(hashes))
	pos := 0
	for level := 0; level <= mask.GetLevel(); level++ {
		if mask.IsSignificant(level) {
			hashes[pos] = c.HashKeyAt(level)
			depths[pos] = c.Depth(level)
			pos++
		}
	}
	return hashes, depths
}

func unpackedMetadata(c *Cell) Metadata {
	mask := c.getLevelMask()
	hashes, depths := collectMetadataHashesDepths(c, mask)
	refs := make([]RefMetadata, c.refsCount())
	view := newCellRefView(c)
	for i := range refs {
		ref := view.logicalBoundaryRef(i)
		if ref == nil {
			refs[i].Lazy = true
			continue
		}
		mask := ref.getLevelMask()
		hashes, depths := collectMetadataHashesDepths(ref, mask)
		refs[i] = RefMetadata{
			Hash: ref.HashKey(), LevelMask: mask, Hashes: hashes, Depths: depths, Lazy: ref.IsLazy(),
		}
	}
	return Metadata{Hash: c.HashKey(), LevelMask: mask, Hashes: hashes, Depths: depths, Refs: refs}
}

func TestGetMetadataPackedMatchesIndependentBuffers(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(5, 3).EndCell()
	base := BeginCell().MustStoreRef(leaf).EndCell()
	pruned, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	multi := BeginCell().MustStoreRef(pruned).MustStoreRef(leaf).EndCell()
	if multi.Level() != 2 {
		t.Fatal("expected a multilevel fixture")
	}
	proof, err := CreateMerkleProof(multi)
	if err != nil {
		t.Fatal(err)
	}
	traceCalls := 0
	trace := NewTrace(TraceHooks{
		OnLoad: func(*Cell) { traceCalls++ }, OnChild: func(int) *Trace { traceCalls++; return nil },
	})
	lazyLoads := 0
	lazy := cellWithLazyRefsFromCell(multi, func(Hash) (*Cell, error) {
		lazyLoads++
		return nil, ErrLazyRefNotFound
	})
	outerProof, err := CreateMerkleProof(proof)
	if err != nil {
		t.Fatal(err)
	}
	bocLazy, err := FromBOCWithOptions(outerProof.ToBOC(), BOCParseOptions{Lazy: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		cell *Cell
	}{
		{"empty", BeginCell().EndCell()},
		{"leaf", leaf},
		{"multilevel", multi},
		{"pruned", pruned},
		{"virtual_level0", multi.Virtualize(0)},
		{"virtual_level1", multi.Virtualize(1)},
		{"merkle_proof", proof},
		{"virtual_proof", proof.Virtualize(0)},
		{"traced", multi.WithTrace(trace)},
		{"lazy_refs", lazy},
		{"virtual_lazy_refs", lazy.Virtualize(0)},
		{"boc_lazy", bocLazy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := tc.cell.GetMetadata(), unpackedMetadata(tc.cell); !reflect.DeepEqual(got, want) {
				t.Fatalf("metadata = %+v, want %+v", got, want)
			}
		})
	}
	if lazyLoads != 0 || traceCalls != 0 {
		t.Fatalf("metadata loaded or traced cells: loads=%d, trace calls=%d", lazyLoads, traceCalls)
	}
}

func TestGetMetadataPackedOwnsIndependentWindows(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(42, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).MustStoreRef(leaf).EndCell()
	got, want := root.GetMetadata(), unpackedMetadata(root)
	got.Hashes = append(got.Hashes, Hash{99})
	got.Depths = append(got.Depths, 99)
	got.Refs[0].Hashes = append(got.Refs[0].Hashes, Hash{98})
	got.Refs[0].Depths = append(got.Refs[0].Depths, 98)
	if got.Refs[0].Hashes[0] != want.Refs[0].Hashes[0] || got.Refs[0].Depths[0] != want.Refs[0].Depths[0] ||
		!reflect.DeepEqual(got.Refs[1], want.Refs[1]) {
		t.Fatal("appending changed another metadata window")
	}

	got = root.GetMetadata()
	got.Hashes[0][0] ^= 0xff
	got.Depths[0]++
	got.Refs[0].Hashes[0][0] ^= 0xff
	got.Refs[0].Depths[0]++
	if !reflect.DeepEqual(got.Refs[1], want.Refs[1]) {
		t.Fatal("equal references share mutable metadata")
	}
	if fresh := root.GetMetadata(); !reflect.DeepEqual(fresh, want) {
		t.Fatal("mutating metadata changed the source cell or a later result")
	}
}

func TestGetMetadataPackedAllocations(t *testing.T) {
	leaf := BeginCell().EndCell()
	root := BeginCell().MustStoreRef(leaf).MustStoreRef(leaf).MustStoreRef(leaf).MustStoreRef(leaf).EndCell()
	if got := testing.AllocsPerRun(100, func() { metadataBenchmarkSink = root.GetMetadata() }); got != 3 {
		t.Fatalf("GetMetadata allocations = %v, want 3", got)
	}
}
