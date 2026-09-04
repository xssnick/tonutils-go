package cell

import (
	"bytes"
	"errors"
	"testing"
)

func TestCloneDetachedOwnsOnlyReachableEagerGraph(t *testing.T) {
	shared := BeginCell().MustStoreUInt(0x5a, 8).EndCell()
	left := BeginCell().MustStoreUInt(1, 1).MustStoreRef(shared).EndCell()
	right := BeginCell().MustStoreUInt(0, 1).MustStoreRef(shared).EndCell()
	root := BeginCell().MustStoreRef(left).MustStoreRef(right).EndCell()
	unrelated := BeginCell().MustStoreUInt(0xdeadbeef, 32).
		MustStoreRef(BeginCell().MustStoreUInt(0xcafe, 16).EndCell()).
		EndCell()
	combinedOptions := BOCSerializeOptions{
		WithCRC32C:    true,
		WithIndex:     true,
		WithCacheBits: true,
		WithIntHashes: true,
	}
	combined := ToBOCWithOptions([]*Cell{root, unrelated}, combinedOptions)
	roots, err := FromBOCMultiRoot(combined)
	if err != nil {
		t.Fatal(err)
	}

	detached, err := roots[0].CloneDetached()
	if err != nil {
		t.Fatal(err)
	}
	if detached.HashKey() != roots[0].HashKey() || detached.Depth() != roots[0].Depth() {
		t.Fatal("detached root metadata changed")
	}
	wantBOC := roots[0].ToBOCWithOptions(combinedOptions)
	if got := detached.ToBOCWithOptions(combinedOptions); !bytes.Equal(got, wantBOC) {
		t.Fatal("detached graph changed canonical BOC serialization")
	}

	sourceReachable := collectDetachedTestGraph(roots[0])
	allSource := collectDetachedTestGraph(roots...)
	detachedCells := collectDetachedTestGraph(detached)
	if len(detachedCells) != len(sourceReachable) {
		t.Fatalf("detached cells = %d, want %d reachable cells", len(detachedCells), len(sourceReachable))
	}
	for cloned := range detachedCells {
		if _, aliases := allSource[cloned]; aliases {
			t.Fatal("detached graph points into the source cell arena")
		}
		for source := range allSource {
			if len(cloned.data) > 0 && len(source.data) > 0 && &cloned.data[0] == &source.data[0] {
				t.Fatal("detached graph points into the source payload arena")
			}
			if cloned.meta != nil && source.meta != nil && cloned.meta.extraHashes != nil &&
				source.meta.extraHashes != nil && cloned.meta.extraHashes == source.meta.extraHashes {
				t.Fatal("detached graph points into the source hash metadata arena")
			}
		}
	}

	assertDetachedTestGraphParity(t, roots[0], detached)
	if detached.refs[0].refs[0] != detached.refs[1].refs[0] {
		t.Fatal("detached graph lost shared-reference identity")
	}
	if _, copied := detachedCells[roots[1]]; copied {
		t.Fatal("detached graph retained the independent second root")
	}
}

func TestCloneDetachedCopiesFinalizedMultiLevelMetadata(t *testing.T) {
	body := BeginCell().MustStoreRef(BeginCell().MustStoreUInt(0xa5, 8).EndCell()).EndCell()
	pruned, err := CreatePrunedBranch(body, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	root := BeginCell().MustStoreRef(pruned).EndCell()
	if root.meta == nil || root.meta.extraHashes == nil {
		t.Fatal("fixture does not carry finalized multi-level metadata")
	}

	detached, err := root.CloneDetached()
	if err != nil {
		t.Fatal(err)
	}
	assertDetachedTestGraphParity(t, root, detached)
	if detached.meta == nil || detached.meta.extraHashes == nil {
		t.Fatal("detached root lost multi-level metadata")
	}
	if detached.meta.extraHashes == root.meta.extraHashes {
		t.Fatal("detached root aliases source multi-level hashes")
	}
}

func TestCloneDetachedRejectsRuntimeViews(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xab, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell()

	lazyRoot, err := FromBOCWithOptions(root.ToBOC(), BOCParseOptions{Lazy: true, TrustedHashes: true})
	if err != nil {
		t.Fatal(err)
	}
	if !lazyRoot.refs[0].IsLazy() {
		t.Fatal("lazy fixture has no lazy boundary")
	}

	pruned, err := CreatePrunedBranch(root, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	virtualized := pruned.Virtualize(0)
	if !virtualized.IsVirtualized() {
		t.Fatal("virtualized fixture is not a view")
	}

	traced := root.WithTrace(NewTrace(TraceHooks{OnLoad: func(*Cell) {}}))
	loaderBacked := root.copy()
	loaderBacked.ensureMeta().lazyLoader = func(Hash) (*Cell, error) { return leaf, nil }
	depthOnlyMeta := root.copy()
	depthOnlyMeta.meta = &cellMeta{extraDepths: [3]uint16{1}}

	for _, test := range []struct {
		name string
		root *Cell
	}{
		{name: "lazy descendant", root: lazyRoot},
		{name: "virtualized root", root: virtualized},
		{name: "traced root", root: traced},
		{name: "loader backed root", root: loaderBacked},
		{name: "incomplete metadata", root: depthOnlyMeta},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.root.CloneDetached(); !errors.Is(err, ErrCannotCloneDetachedCell) {
				t.Fatalf("CloneDetached error = %v, want ErrCannotCloneDetachedCell", err)
			}
		})
	}
}

func collectDetachedTestGraph(roots ...*Cell) map[*Cell]struct{} {
	seen := make(map[*Cell]struct{})
	queue := append([]*Cell(nil), roots...)
	for len(queue) > 0 {
		cell := queue[0]
		queue = queue[1:]
		if _, exists := seen[cell]; exists {
			continue
		}

		seen[cell] = struct{}{}
		queue = append(queue, cell.rawRefs()...)
	}

	return seen
}

func assertDetachedTestGraphParity(t *testing.T, source, detached *Cell) {
	t.Helper()

	type pair struct {
		source   *Cell
		detached *Cell
	}
	queue := []pair{{source: source, detached: detached}}
	clones := make(map[*Cell]*Cell)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if existing, seen := clones[current.source]; seen {
			if existing != current.detached {
				t.Fatal("shared source cell was duplicated")
			}
			continue
		}
		clones[current.source] = current.detached

		if current.source == current.detached {
			t.Fatal("detached cell aliases source cell")
		}
		if current.source.bitsSz != current.detached.bitsSz ||
			current.source.hash0 != current.detached.hash0 ||
			current.source.depth0 != current.detached.depth0 ||
			current.source.flags != current.detached.flags ||
			current.source.typ != current.detached.typ ||
			!bytes.Equal(current.source.data, current.detached.data) {
			t.Fatal("detached cell fields changed")
		}
		if current.source.meta != nil && current.source.meta.extraHashes != nil {
			// Only the slots the level mask makes significant are compared: the
			// storage is packed (see prewireParsedExtraHashes), so the rest of a
			// window belongs to other cells on both sides.
			slots := current.source.extraHashSlots()
			if current.detached.meta == nil || current.detached.meta.extraHashes == nil ||
				!bytes.Equal(extraHashBytes(current.source.meta.extraHashes, slots),
					extraHashBytes(current.detached.meta.extraHashes, slots)) ||
				current.source.meta.extraDepths != current.detached.meta.extraDepths {
				t.Fatal("detached cell hash/depth metadata changed")
			}
		} else if current.detached.meta != nil {
			t.Fatal("detached cell gained metadata")
		}

		if current.source.refsCount() != current.detached.refsCount() {
			t.Fatal("detached cell reference count changed")
		}
		for i := 0; i < current.source.refsCount(); i++ {
			queue = append(queue, pair{source: current.source.refs[i], detached: current.detached.refs[i]})
		}
	}
}

// extraHashBytes is the significant prefix of a packed extra-hash window as
// bytes, for comparisons that must not look past a cell's own slots.
func extraHashBytes(window *[3]Hash, slots int) []byte {
	out := make([]byte, 0, slots*hashSize)
	for i := 0; i < slots; i++ {
		out = append(out, window[i][:]...)
	}
	return out
}

// TestCloneDetachedSizedIsTheSameGraphForEveryHint holds the property the
// hint is documented with: it sizes scratch structures and changes nothing
// about the clone. A hint below the true count, one equal to it and one far
// above it must all produce the graph CloneDetached does, out of a parsed
// arena whose level>0 cells carry packed hash windows.
func TestCloneDetachedSizedIsTheSameGraphForEveryHint(t *testing.T) {
	var counter uint64
	tree := buildFinalizeParityTree(t, 3, &counter)
	skeleton := CreateProofSkeleton()
	skeleton.ProofRef(0).ProofRef(1).SetRecursive()
	skeleton.ProofRef(2).ProofRef(3).ProofRef(0).SetRecursive()
	proof, err := tree.CreateProof(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := BeginCell().MustStoreUInt(0xdeadbeef, 32).EndCell()
	combined := ToBOCWithOptions([]*Cell{proof, unrelated}, BOCSerializeOptions{WithCRC32C: true})
	roots, err := FromBOCMultiRootWithOptions(combined, BOCParseOptions{NoCopyPayload: true})
	if err != nil {
		t.Fatal(err)
	}
	source := roots[0]
	reachable := len(collectDetachedTestGraph(source))
	if reachable < 8 {
		t.Fatalf("fixture proof has %d cells, too small to exercise sizing", reachable)
	}

	want, err := source.CloneDetached()
	if err != nil {
		t.Fatal(err)
	}
	wantBOC := want.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true, WithIntHashes: true})
	for _, hint := range []int{-1, 0, 1, reachable / 2, reachable, reachable * 4, 1 << 20} {
		got, err := source.CloneDetachedSized(hint)
		if err != nil {
			t.Fatalf("hint %d: %v", hint, err)
		}
		assertDetachedTestGraphParity(t, source, got)
		if gotBOC := got.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true, WithIntHashes: true}); !bytes.Equal(gotBOC, wantBOC) {
			t.Fatalf("hint %d: detached graph serialization differs from the unsized clone", hint)
		}
		if err := CheckProof(got, tree.Hash()); err != nil {
			t.Fatalf("hint %d: detached proof no longer verifies: %v", hint, err)
		}
	}
}
