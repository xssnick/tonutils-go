package cell

import (
	"bytes"
	"fmt"
	"testing"
)

func TestCreateMerkleUpdateAppliedOwnsDestinationCells(t *testing.T) {
	t.Parallel()

	type ownershipCase struct {
		name    string
		read    *ReadSet
		to      *Cell
		sharing bool
	}

	fixture := newPrunedUpdateFixture(t, 6, 8, 2026091701)
	source := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	read := NewReadSet(source)
	if _, err := read.Root().BeginParse(); err != nil {
		t.Fatal(err)
	}
	leaf := BeginCell().MustStoreUInt(0x22, 8).EndCell()
	library, err := BeginCell().MustStoreUInt(uint64(LibraryCellType), 8).
		MustStoreSlice(bytes.Repeat([]byte{0x33}, 32), 256).EndCellSpecial(true)
	if err != nil {
		t.Fatal(err)
	}
	branch := BeginCell().MustStoreUInt(0x44, 8).
		MustStoreRef(leaf).MustStoreRef(leaf).EndCell()
	whole := BeginCell().MustStoreUInt(0x55, 8).
		MustStoreRef(branch).MustStoreRef(library).EndCell()

	for _, tc := range []ownershipCase{
		{name: "source boundaries", read: fixture.read, to: fixture.to, sharing: true},
		{name: "all new shared and exotic cells", read: read, to: whole},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A separately rooted tree models collated data in the same parser
			// allocation. No new state cell may keep that allocation alive.
			boc := ToBOCWithFlags([]*Cell{tc.to, prunedUpdateTree(4, 1000)}, false)
			roots, err := FromBOCMultiRootWithOptions(boc, BOCParseOptions{NoCopyPayload: true})
			if err != nil {
				t.Fatal(err)
			}
			wantUpdate, err := tc.read.CreateMerkleUpdate(roots[0])
			if err != nil {
				t.Fatal(err)
			}
			wantUpdateBOC := wantUpdate.ToBOC()
			wantStateBOC := roots[0].ToBOC()

			for _, workers := range []int{1, 8} {
				t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
					update, applied, _, err := tc.read.CreateMerkleUpdateAppliedSized(
						roots[0], proofParallelMinCells*4, workers,
					)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(update.ToBOC(), wantUpdateBOC) {
						t.Fatal("owning the applied state changed the serialized update")
					}
					if !bytes.Equal(applied.ToBOC(), wantStateBOC) {
						t.Fatal("owning the applied state changed the destination")
					}
					assertAppliedCellsOwnedOutsideSource(t, applied, tc.read.Source(), roots...)
					if tc.sharing && countSharedCells(applied, tc.read.Source()) == 0 {
						t.Fatal("unchanged source boundaries lost pointer identity")
					}
					// Parallel workers intentionally have separate destination memos.
					if !tc.sharing && workers == 1 && applied.refs[0].refs[0] != applied.refs[0].refs[1] {
						t.Fatal("shared destination leaf was duplicated")
					}
					assertNoTrace(t, applied)
				})
			}
		})
	}
}

func TestCreateMerkleUpdateAppliedOwnsChildlessDestination(t *testing.T) {
	t.Parallel()

	source := BeginCell().MustStoreUInt(1, 8).EndCell()
	read := NewReadSet(source)
	if _, err := read.Root().BeginParse(); err != nil {
		t.Fatal(err)
	}
	destination := BeginCell().MustStoreUInt(2, 8).EndCell()
	boc := ToBOCWithFlags([]*Cell{destination, prunedUpdateTree(4, 1000)}, false)
	roots, err := FromBOCMultiRootWithOptions(boc, BOCParseOptions{NoCopyPayload: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 8} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			_, applied, _, err := read.CreateMerkleUpdateAppliedSized(
				roots[0], proofParallelMinCells, workers,
			)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(applied.ToBOC(), destination.ToBOC()) {
				t.Fatal("owned leaf changed bytes")
			}
			assertAppliedCellsOwnedOutsideSource(t, applied, source, roots...)
		})
	}
}

func TestAppliedOwnershipPreservesVirtualPrunedLeaf(t *testing.T) {
	t.Parallel()

	source := BeginCell().MustStoreUInt(1, 8).EndCell()
	hidden := BeginCell().MustStoreRef(BeginCell().MustStoreUInt(2, 8).EndCell()).EndCell()
	pruned, err := CreatePrunedBranch(hidden, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	virtual := pruned.Virtualize(0)
	update := mustMerkleUpdateCell(t, source, virtual)
	want, err := benchmarkApplyMerkleUpdateBaseline(source, update)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := ApplyMerkleUpdate(source, update)
	if err != nil {
		t.Fatal(err)
	}
	if owned == virtual || owned.rawCell() == virtual.rawCell() {
		t.Fatal("owned virtual leaf retains the old view or raw cell")
	}
	if &owned.data[0] == &virtual.data[0] {
		t.Fatal("owned virtual leaf retains the old payload")
	}
	if !owned.IsVirtualized() || owned.EffectiveLevel() != want.EffectiveLevel() ||
		owned.ActualLevel() != want.ActualLevel() || owned.LevelMask() != want.LevelMask() {
		t.Fatal("owning the virtual leaf changed its view")
	}
	for level := 0; level <= _DataCellMaxLevel; level++ {
		if owned.HashKeyAt(level) != want.HashKeyAt(level) || owned.Depth(level) != want.Depth(level) {
			t.Fatalf("owned virtual leaf differs from classic apply at level %d", level)
		}
	}
}

func assertAppliedCellsOwnedOutsideSource(t *testing.T, applied, source *Cell, temporary ...*Cell) {
	t.Helper()

	borrowed := collectDetachedTestGraph(temporary...)
	borrowedData := make(map[*byte]struct{}, len(borrowed))
	borrowedMeta := make(map[*cellMeta]struct{})
	borrowedHashes := make(map[*[3]Hash]struct{})
	for c := range borrowed {
		if len(c.data) > 0 {
			borrowedData[&c.data[0]] = struct{}{}
		}
		if c.meta != nil {
			borrowedMeta[c.meta] = struct{}{}
			if c.meta.extraHashes != nil {
				borrowedHashes[c.meta.extraHashes] = struct{}{}
			}
		}
	}
	old := collectDetachedTestGraph(source)
	owned := 0
	for c := range collectDetachedTestGraph(applied) {
		if _, exists := old[c]; exists {
			continue
		}
		owned++
		if _, exists := borrowed[c]; exists {
			t.Fatal("applied destination cell retains the temporary cell allocation")
		}
		if len(c.data) > 0 {
			if _, exists := borrowedData[&c.data[0]]; exists {
				t.Fatal("applied destination data retains the temporary payload")
			}
		}
		if c.meta != nil {
			if _, exists := borrowedMeta[c.meta]; exists {
				t.Fatal("applied destination metadata aliases temporary metadata")
			}
			if _, exists := borrowedHashes[c.meta.extraHashes]; exists {
				t.Fatal("applied destination hashes alias temporary metadata")
			}
			if c.meta.viewOf != nil || c.meta.lazyLoader != nil || c.meta.trace != nil {
				t.Fatal("owned destination cell retains a runtime owner")
			}
		}
	}
	if owned == 0 {
		t.Fatal("fixture exercised no new destination cells")
	}
}
