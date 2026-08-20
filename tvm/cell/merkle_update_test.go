package cell

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func TestMerkleUpdateHands(t *testing.T) {
	data := BeginCell().
		MustStoreUInt(0xAA, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0xBB, 8).EndCell()).
		EndCell()
	node := BeginCell().
		MustStoreUInt(0x10, 8).
		MustStoreRef(data).
		EndCell()
	otherNode := BeginCell().
		MustStoreUInt(0x11, 8).
		MustStoreRef(data).
		EndCell()

	prunedData, err := createPrunedBranchFromCell(data, 1)
	if err != nil {
		t.Fatalf("failed to create pruned data branch: %v", err)
	}

	updateFrom := BeginCell().
		MustStoreUInt(0x10, 8).
		MustStoreRef(prunedData).
		EndCell()
	newNode := BeginCell().
		MustStoreUInt(0x20, 8).
		MustStoreRef(data).
		EndCell()
	updateTo := BeginCell().
		MustStoreUInt(0x20, 8).
		MustStoreRef(prunedData).
		EndCell()
	update := mustMerkleUpdateCell(t, updateFrom, updateTo)

	if err := MayApplyMerkleUpdate(node, update); err != nil {
		t.Fatalf("may apply failed: %v", err)
	}
	if err := ValidateMerkleUpdate(update); err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	got, err := ApplyMerkleUpdate(node, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	assertCellsEqual(t, got, newNode)
	// The unchanged subtree must be the source cell itself, at the same ref index.
	if got.ref(0) != data {
		t.Fatalf("ref 0 is %x, want the source cell %x", got.ref(0).Hash()[:8], data.Hash()[:8])
	}

	if err := MayApplyMerkleUpdate(otherNode, update); err == nil {
		t.Fatal("expected may apply to reject a mismatched source root")
	}
	if _, err := ApplyMerkleUpdate(otherNode, update); err == nil {
		t.Fatal("expected apply to reject a mismatched source root")
	}

	prunedOtherNode, err := createPrunedBranchFromCell(otherNode, 1)
	if err != nil {
		t.Fatalf("failed to create pruned other node: %v", err)
	}
	prunedNewNode, err := createPrunedBranchFromCell(newNode, 1)
	if err != nil {
		t.Fatalf("failed to create pruned new node: %v", err)
	}
	otherUpdate := mustMerkleUpdateCell(t, prunedOtherNode, prunedNewNode)

	if err := MayApplyMerkleUpdate(node, otherUpdate); err == nil {
		t.Fatal("expected may apply to reject a mismatched pruned source root")
	}
	if err := ValidateMerkleUpdate(otherUpdate); err == nil {
		t.Fatal("expected validate to reject unknown pruned destination branches")
	}
	if _, err := ApplyMerkleUpdate(otherNode, otherUpdate); err == nil {
		t.Fatal("expected apply to reject unknown pruned destination branches")
	}
}

func TestCreateMerkleUpdateCppGoldenRecordedHands(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xE, 4).EndCell()
	reused := BeginCell().MustStoreUInt(0xC0DE, 16).MustStoreRef(leaf).EndCell()
	from := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(reused).EndCell()

	rs := NewReadSet(from)
	reusedRef, err := rs.Root().MustBeginParse().PeekRefCellAt(0)
	if err != nil {
		t.Fatal(err)
	}
	to := BeginCell().MustStoreUInt(0xB2, 8).MustStoreRef(reusedRef).EndCell()

	update, err := rs.CreateMerkleUpdate(to)
	if err != nil {
		t.Fatal(err)
	}

	prunedReused, err := createPrunedBranchFromCell(reused, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedFrom := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(prunedReused).EndCell()
	expectedTo := BeginCell().MustStoreUInt(0xB2, 8).MustStoreRef(prunedReused).EndCell()
	expectedUpdate := mustMerkleUpdateCell(t, expectedFrom, expectedTo)
	if !bytes.Equal(update.ToBOCWithOptions(BOCSerializeOptions{}), expectedUpdate.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatalf("recorded merkle update BOC mismatch:\n got: %x\nwant: %x", update.ToBOCWithOptions(BOCSerializeOptions{}), expectedUpdate.ToBOCWithOptions(BOCSerializeOptions{}))
	}

	if err := MayApplyMerkleUpdate(from, update); err != nil {
		t.Fatalf("may apply failed: %v", err)
	}
	if err := ValidateMerkleUpdate(update); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	got, err := ApplyMerkleUpdate(from, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	assertCellsEqual(t, got, to)
	if countSharedCells(got, reused) == 0 {
		t.Fatal("the unchanged subtree was copied instead of being reused from the source")
	}
}

func TestCreateMerkleUpdateDoesNotReuseChangedCellWithSourceTrace(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xEE, 8).EndCell()
	from := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(leaf).EndCell()

	rs := NewReadSet(from)
	if _, err := rs.Root().BeginParse(); err != nil {
		t.Fatal(err)
	}

	to := BeginCell().
		MustStoreUInt(0xB2, 8).
		MustStoreRef(leaf).
		EndCell().
		WithTrace(rs.Trace())
	update, err := rs.CreateMerkleUpdate(to)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ApplyMerkleUpdate(from, update)
	if err != nil {
		t.Fatal(err)
	}
	assertCellsEqual(t, got, to)
}

// Every call decides its boundaries from the destination it was handed. The
// record only ever grows, so a second update sees at least as much of the source
// as the first one did — but the source proof it emits exposes the boundaries
// that destination reused and nothing else, because a boundary the caller did
// not hand back must stay pruned to keep the proof at the size the reads justify.
func TestCreateMerkleUpdateRepeatedCallsScopeBoundariesToTheirDestination(t *testing.T) {
	leafA := BeginCell().MustStoreUInt(0xA, 4).EndCell()
	leafB := BeginCell().MustStoreUInt(0xB, 4).EndCell()
	sharedA := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(leafA).EndCell()
	sharedB := BeginCell().MustStoreUInt(0xB2, 8).MustStoreRef(leafB).EndCell()
	branchA := BeginCell().MustStoreUInt(0x1A, 8).MustStoreRef(sharedA).EndCell()
	branchB := BeginCell().MustStoreUInt(0x1B, 8).MustStoreRef(sharedB).EndCell()
	from := BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(branchA).MustStoreRef(branchB).EndCell()

	rs := NewReadSet(from)
	rootSlice := rs.Root().MustBeginParse()
	readBranchA, err := rootSlice.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	readBranchB, err := rootSlice.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	readSharedA, err := readBranchA.PeekRefCellAt(0)
	if err != nil {
		t.Fatal(err)
	}
	readSharedB, err := readBranchB.PeekRefCellAt(0)
	if err != nil {
		t.Fatal(err)
	}

	prunedSharedA, err := createPrunedBranchFromCell(sharedA, 1)
	if err != nil {
		t.Fatal(err)
	}
	prunedSharedB, err := createPrunedBranchFromCell(sharedB, 1)
	if err != nil {
		t.Fatal(err)
	}
	prunedBranchA, err := createPrunedBranchFromCell(branchA, 1)
	if err != nil {
		t.Fatal(err)
	}
	prunedBranchB, err := createPrunedBranchFromCell(branchB, 1)
	if err != nil {
		t.Fatal(err)
	}
	keptBranchA := BeginCell().MustStoreUInt(0x1A, 8).MustStoreRef(prunedSharedA).EndCell()
	keptBranchB := BeginCell().MustStoreUInt(0x1B, 8).MustStoreRef(prunedSharedB).EndCell()

	toA := BeginCell().MustStoreUInt(0xDA, 8).MustStoreRef(readSharedA).EndCell()
	updateFromA, _, _, _, err := rs.createMerkleUpdateRaw(toA, false, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedFromA := BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(keptBranchA).MustStoreRef(prunedBranchB).EndCell()
	if !bytes.Equal(updateFromA.ToBOCWithOptions(BOCSerializeOptions{}), expectedFromA.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatalf("first update source proof mismatch:\n got: %x\nwant: %x",
			updateFromA.ToBOCWithOptions(BOCSerializeOptions{}), expectedFromA.ToBOCWithOptions(BOCSerializeOptions{}))
	}

	toB := BeginCell().MustStoreUInt(0xDB, 8).MustStoreRef(readSharedB).EndCell()
	updateFromB, _, _, _, err := rs.createMerkleUpdateRaw(toB, false, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedFromB := BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(prunedBranchA).MustStoreRef(keptBranchB).EndCell()
	if !bytes.Equal(updateFromB.ToBOCWithOptions(BOCSerializeOptions{}), expectedFromB.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatalf("second update source proof mismatch:\n got: %x\nwant: %x",
			updateFromB.ToBOCWithOptions(BOCSerializeOptions{}), expectedFromB.ToBOCWithOptions(BOCSerializeOptions{}))
	}
}

// The same subtree hangs under two parents and the destination hands both copies
// back. There is one boundary, because a boundary is a hash, and exposing it once
// is enough: applying an update looks a boundary up by hash and takes whichever
// occurrence the source proof carries. So the first parent is kept with the
// subtree pruned under it, and the second is cut away whole.
//
// Opening a path to every occurrence instead is what a hash-keyed recorder does
// if it lets a shared subtree answer "there is a boundary below me" once per
// parent. It stays valid — every boundary is still reachable — and it is what this
// package used to emit; measured against the reference collator on a real
// masterchain-referenced block, it made the source half of the update carry 581
// body cells it did not need, and the block 22 KB larger.
//
// Which of the two parents carries it is a choice, not a law: both shapes apply
// to the same new state. The claim pass hands a contested subtree to the parent
// it reaches first in reverse post-order, which is the last one the walk
// finished — parent1 here. On a shard state that rule puts the claim on the
// accounts side rather than the out-message queue, because accounts is the root's
// last child and holds all but a handful of the boundaries; measured on the same
// real block it took the source half from 4274 cells to 4260, against the
// reference's 4265, and the block from 426021 to 425674 bytes.
func TestCreateMerkleUpdateSharedDAGExposesBoundaryOnce(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xEE, 8).EndCell()
	shared := BeginCell().MustStoreUInt(0x5A, 8).MustStoreRef(leaf).EndCell()
	parent0 := BeginCell().MustStoreUInt(0x10, 8).MustStoreRef(shared).EndCell()
	parent1 := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(shared).EndCell()
	from := BeginCell().MustStoreUInt(0xA0, 8).MustStoreRef(parent0).MustStoreRef(parent1).EndCell()

	rs := NewReadSet(from)
	rootSlice := rs.Root().MustBeginParse()
	readParent0, err := rootSlice.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	readParent1, err := rootSlice.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	readShared0, err := readParent0.PeekRefCellAt(0)
	if err != nil {
		t.Fatal(err)
	}
	readShared1, err := readParent1.PeekRefCellAt(0)
	if err != nil {
		t.Fatal(err)
	}

	newParent0 := BeginCell().MustStoreUInt(0x20, 8).MustStoreRef(readShared0).EndCell()
	newParent1 := BeginCell().MustStoreUInt(0x21, 8).MustStoreRef(readShared1).EndCell()
	to := BeginCell().MustStoreUInt(0xB0, 8).MustStoreRef(newParent0).MustStoreRef(newParent1).EndCell()
	update, err := rs.CreateMerkleUpdate(to)
	if err != nil {
		t.Fatal(err)
	}

	prunedShared, err := createPrunedBranchFromCell(shared, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedParent1 := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(prunedShared).EndCell()
	// The other parent proves nothing this one has not already proven, so the
	// source proof cuts it away instead of repeating the path.
	expectedParent0, err := createPrunedBranchFromCell(parent0, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedFrom := BeginCell().MustStoreUInt(0xA0, 8).MustStoreRef(expectedParent0).MustStoreRef(expectedParent1).EndCell()
	expectedNewParent0 := BeginCell().MustStoreUInt(0x20, 8).MustStoreRef(prunedShared).EndCell()
	expectedNewParent1 := BeginCell().MustStoreUInt(0x21, 8).MustStoreRef(prunedShared).EndCell()
	expectedTo := BeginCell().MustStoreUInt(0xB0, 8).MustStoreRef(expectedNewParent0).MustStoreRef(expectedNewParent1).EndCell()
	expectedUpdate, err := CreateMerkleUpdate(expectedFrom, expectedTo)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(update.ToBOCWithOptions(BOCSerializeOptions{}), expectedUpdate.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatalf("shared-DAG merkle update BOC mismatch:\n got: %x\nwant: %x",
			update.ToBOCWithOptions(BOCSerializeOptions{}), expectedUpdate.ToBOCWithOptions(BOCSerializeOptions{}))
	}

	checkMerkleUpdate(t, from, to, update)
}

func TestCreateMerkleUpdateCppGoldenNoReusePrunesSourceRoot(t *testing.T) {
	from := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(BeginCell().MustStoreUInt(0x11, 8).EndCell()).EndCell()
	to := BeginCell().MustStoreUInt(0xB2, 8).MustStoreRef(BeginCell().MustStoreUInt(0x22, 8).EndCell()).EndCell()

	rs := NewReadSet(from)
	if _, err := rs.Root().BeginParse(); err != nil {
		t.Fatal(err)
	}

	update, err := rs.CreateMerkleUpdate(to)
	if err != nil {
		t.Fatal(err)
	}

	prunedFrom, err := createPrunedBranchFromCell(from, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedUpdate := mustMerkleUpdateCell(t, prunedFrom, to)
	if !bytes.Equal(update.ToBOCWithOptions(BOCSerializeOptions{}), expectedUpdate.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatalf("no-reuse merkle update BOC mismatch:\n got: %x\nwant: %x", update.ToBOCWithOptions(BOCSerializeOptions{}), expectedUpdate.ToBOCWithOptions(BOCSerializeOptions{}))
	}

	got, err := ApplyMerkleUpdate(from, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	assertCellsEqual(t, got, to)
}

func TestMerkleUpdateOrdinaryTreeCases(t *testing.T) {
	fromValues := []uint16{0, 1, 2, 3, 4, 5, 6, 7}

	for leaf := 0; leaf < len(fromValues); leaf++ {
		t.Run(binaryPathName(leaf, 3), func(t *testing.T) {
			toValues := append([]uint16{}, fromValues...)
			toValues[leaf] = uint16(100 + leaf)

			from := buildBinaryTree(fromValues)
			to := buildBinaryTree(toValues)
			path := binaryPath(leaf, 3)

			updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodies(from, to, path)
			if err != nil {
				t.Fatalf("failed to build merkle update bodies: %v", err)
			}
			update := mustMerkleUpdateCell(t, updateFrom, updateTo)

			if err := ValidateMerkleUpdate(update); err != nil {
				t.Fatalf("validate failed: %v", err)
			}
			if err := MayApplyMerkleUpdate(from, update); err != nil {
				t.Fatalf("may apply failed: %v", err)
			}

			got, err := ApplyMerkleUpdate(from, update)
			if err != nil {
				t.Fatalf("apply failed: %v", err)
			}
			assertCellsEqual(t, got, to)
		})
	}
}

func TestApplyMerkleUpdateReusesUnchangedRefs(t *testing.T) {
	from := buildBinaryTree([]uint16{1, 2, 3, 4})
	to := buildBinaryTree([]uint16{101, 2, 3, 4})

	updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(from, to)
	if err != nil {
		t.Fatalf("failed to build merkle update bodies: %v", err)
	}
	update := mustMerkleUpdateCell(t, updateFrom, updateTo)

	got, err := ApplyMerkleUpdate(from, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	assertCellsEqual(t, got, to)
	if got.ref(1) != from.ref(1) {
		t.Fatal("expected unchanged right subtree to reuse source reference identity")
	}
}

func TestApplyMerkleUpdateDefaultPathLoadsLazyRefs(t *testing.T) {
	from := buildBinaryTree([]uint16{1, 2, 3, 4})
	to := buildBinaryTree([]uint16{101, 2, 3, 4})

	updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(from, to)
	if err != nil {
		t.Fatalf("failed to build merkle update bodies: %v", err)
	}
	update := mustMerkleUpdateCell(t, updateFrom, updateTo)

	loader := testLazyLoaderForCells(from.rawRefs()...)
	lazyFrom := cellWithLazyRefsFromCell(from, loader.LoadCell)

	got, err := ApplyMerkleUpdate(lazyFrom, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if got.HashKey() != to.HashKey() {
		t.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), to.Hash())
	}
	if loader.calls != 1 {
		t.Fatalf("expected default path to load changed source path once, got %d loads", loader.calls)
	}
}

func TestApplyMerkleUpdateCollectsKnownBranchesWhenDestinationShapeDiffers(t *testing.T) {
	leftLeaf := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	rightLeaf := BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	left := BeginCell().MustStoreRef(leftLeaf).EndCell()
	right := BeginCell().MustStoreRef(rightLeaf).EndCell()
	from := BeginCell().
		MustStoreUInt(0x10, 8).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()
	to := BeginCell().
		MustStoreUInt(0x20, 8).
		MustStoreRef(left).
		EndCell()

	prunedLeft, err := createPrunedBranchFromCell(left, 1)
	if err != nil {
		t.Fatalf("failed to prune left subtree: %v", err)
	}
	prunedRight, err := createPrunedBranchFromCell(right, 1)
	if err != nil {
		t.Fatalf("failed to prune right subtree: %v", err)
	}
	updateFrom, err := copyCellWithRefs(from, []*Cell{prunedLeft, prunedRight})
	if err != nil {
		t.Fatalf("failed to build pruned source: %v", err)
	}
	updateTo, err := copyCellWithRefs(to, []*Cell{prunedLeft})
	if err != nil {
		t.Fatalf("failed to build pruned destination: %v", err)
	}
	update := mustMerkleUpdateCell(t, updateFrom, updateTo)

	lazyFrom := cellWithLazyRefsFromCell(from)

	got, err := ApplyMerkleUpdate(lazyFrom, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if got.HashKey() != to.HashKey() {
		t.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), to.Hash())
	}
	if got.ref(0).HashKey() != left.HashKey() {
		t.Fatalf("expected the unchanged left subtree, got %x", got.ref(0).Hash()[:8])
	}
	if !got.ref(0).IsLazy() {
		t.Fatal("expected reused left ref to remain lazy")
	}
}

// An update naming a pruned boundary the source does not carry cannot be applied,
// and must fail rather than return a partially rebuilt root.
func TestApplyMerkleUpdateRejectsUnknownPrunedBranch(t *testing.T) {
	known := BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(BeginCell().EndCell()).EndCell()
	oldOther := BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(BeginCell().EndCell()).EndCell()
	unknown := BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(BeginCell().EndCell()).EndCell()
	from := BeginCell().MustStoreRef(known).MustStoreRef(oldOther).EndCell()

	prunedKnown, err := createPrunedBranchFromCell(known, 1)
	if err != nil {
		t.Fatal(err)
	}
	prunedUnknown, err := createPrunedBranchFromCell(unknown, 1)
	if err != nil {
		t.Fatal(err)
	}
	updateTo := BeginCell().MustStoreRef(prunedKnown).MustStoreRef(prunedUnknown).EndCell()
	update := mustMerkleUpdateCell(t, from, updateTo)

	got, err := ApplyMerkleUpdate(from, update)
	if err == nil {
		t.Fatal("update with an unknown pruned branch succeeded")
	}
	if got != nil {
		t.Fatal("failed update returned a root")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%x", unknown.Hash())) {
		t.Fatalf("error does not name the unknown branch: %v", err)
	}
}

func TestValidateMerkleUpdateLoadsLazyRootRefs(t *testing.T) {
	from := buildBinaryTree([]uint16{1, 2, 3, 4})
	to := buildBinaryTree([]uint16{1, 2, 3, 44})

	updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(from, to)
	if err != nil {
		t.Fatalf("failed to build merkle update bodies: %v", err)
	}

	update := mustMerkleUpdateCell(t,
		mustCreateLazyPrunedRef(t, lazyRefFromCell(updateFrom)),
		mustCreateLazyPrunedRef(t, lazyRefFromCell(updateTo)),
	)
	if err = ValidateMerkleUpdate(update); !errors.Is(err, ErrLazyLoaderNotSet) {
		t.Fatalf("expected lazy loader error, got %v", err)
	}

	loader := testLazyLoaderForCells(updateFrom, updateTo)
	update = mustMerkleUpdateCell(t,
		mustCreateLazyPrunedRef(t, lazyRefFromCell(updateFrom), loader.LoadCell),
		mustCreateLazyPrunedRef(t, lazyRefFromCell(updateTo), loader.LoadCell),
	)

	if err = ValidateMerkleUpdate(update); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if loader.calls == 0 {
		t.Fatal("expected lazy update refs to be loaded")
	}
}

func TestMerkleUpdateRandomOrdinaryTrees(t *testing.T) {
	rnd := rand.New(rand.NewSource(123))

	for i := 0; i < 128; i++ {
		fromValues := randomLeafValues(rnd, 16)
		toValues := mutateLeafValues(rnd, fromValues, 1+rnd.Intn(4))

		from := buildBinaryTree(fromValues)
		to := buildBinaryTree(toValues)
		updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(from, to)
		if err != nil {
			t.Fatalf("failed to build merkle update bodies: %v", err)
		}

		checkMerkleUpdate(t, from, to, mustMerkleUpdateCell(t, updateFrom, updateTo))
	}
}

func TestCombineMerkleUpdateHands(t *testing.T) {
	data := BeginCell().
		MustStoreUInt(0xAA, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0xBB, 8).EndCell()).
		EndCell()
	node := BeginCell().
		MustStoreUInt(0x10, 8).
		MustStoreRef(data).
		EndCell()
	newNode := BeginCell().
		MustStoreUInt(0x20, 8).
		MustStoreRef(data).
		EndCell()
	finalNode := BeginCell().
		MustStoreUInt(0x30, 8).
		MustStoreRef(data).
		EndCell()

	prunedData, err := createPrunedBranchFromCell(data, 1)
	if err != nil {
		t.Fatalf("failed to create pruned data branch: %v", err)
	}
	updateAB := mustMerkleUpdateCell(t,
		BeginCell().MustStoreUInt(0x10, 8).MustStoreRef(prunedData).EndCell(),
		BeginCell().MustStoreUInt(0x20, 8).MustStoreRef(prunedData).EndCell(),
	)
	updateBC := mustMerkleUpdateCell(t,
		BeginCell().MustStoreUInt(0x20, 8).MustStoreRef(prunedData).EndCell(),
		BeginCell().MustStoreUInt(0x30, 8).MustStoreRef(prunedData).EndCell(),
	)

	combined, err := CombineMerkleUpdate(updateAB, updateBC)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	checkMerkleUpdate(t, node, finalNode, combined)

	prunedNewNode, err := createPrunedBranchFromCell(newNode, 1)
	if err != nil {
		t.Fatalf("failed to create pruned new node: %v", err)
	}
	prunedOtherNode, err := createPrunedBranchFromCell(BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(data).EndCell(), 1)
	if err != nil {
		t.Fatalf("failed to create pruned other node: %v", err)
	}
	badUpdate := mustMerkleUpdateCell(t, prunedNewNode, prunedOtherNode)
	if _, err := CombineMerkleUpdate(updateAB, badUpdate); err == nil {
		t.Fatal("expected combine to reject invalid right-side update")
	}
}

func TestCombineMerkleUpdateOrdinaryTreeCases(t *testing.T) {
	base := []uint16{0, 1, 2, 3, 4, 5, 6, 7}

	for first := 0; first < len(base); first++ {
		for second := 0; second < len(base); second++ {
			t.Run(binaryPathName(first, 3)+"_"+binaryPathName(second, 3), func(t *testing.T) {
				aValues := append([]uint16{}, base...)
				bValues := append([]uint16{}, aValues...)
				bValues[first] = uint16(100 + first)
				cValues := append([]uint16{}, bValues...)
				cValues[second] = uint16(200 + second)

				a := buildBinaryTree(aValues)
				b := buildBinaryTree(bValues)
				c := buildBinaryTree(cValues)

				updateABFrom, updateABTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(a, b)
				if err != nil {
					t.Fatalf("failed to build AB update bodies: %v", err)
				}
				updateBCFrom, updateBCTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(b, c)
				if err != nil {
					t.Fatalf("failed to build BC update bodies: %v", err)
				}

				combined, err := CombineMerkleUpdate(
					mustMerkleUpdateCell(t, updateABFrom, updateABTo),
					mustMerkleUpdateCell(t, updateBCFrom, updateBCTo),
				)
				if err != nil {
					t.Fatalf("combine failed: %v", err)
				}
				checkMerkleUpdate(t, a, c, combined)
			})
		}
	}
}

func TestCombineMerkleUpdateRandomOrdinaryTrees(t *testing.T) {
	rnd := rand.New(rand.NewSource(321))

	for i := 0; i < 128; i++ {
		aValues := randomLeafValues(rnd, 16)
		bValues := mutateLeafValues(rnd, aValues, 1+rnd.Intn(4))
		cValues := mutateLeafValues(rnd, bValues, 1+rnd.Intn(4))

		a := buildBinaryTree(aValues)
		b := buildBinaryTree(bValues)
		c := buildBinaryTree(cValues)

		updateABFrom, updateABTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(a, b)
		if err != nil {
			t.Fatalf("failed to build AB update bodies: %v", err)
		}
		updateBCFrom, updateBCTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(b, c)
		if err != nil {
			t.Fatalf("failed to build BC update bodies: %v", err)
		}

		combined, err := CombineMerkleUpdate(
			mustMerkleUpdateCell(t, updateABFrom, updateABTo),
			mustMerkleUpdateCell(t, updateBCFrom, updateBCTo),
		)
		if err != nil {
			t.Fatalf("combine failed: %v", err)
		}
		checkMerkleUpdate(t, a, c, combined)
	}
}

func TestCombineMerkleUpdateSharedSubtrees(t *testing.T) {
	leafA := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	leafB := BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	leafC := BeginCell().MustStoreUInt(0xCC, 8).EndCell()

	sharedA := BeginCell().MustStoreUInt(0x10, 8).MustStoreRef(leafA).EndCell()
	sharedB := BeginCell().MustStoreUInt(0x20, 8).MustStoreRef(leafB).EndCell()
	sharedC := BeginCell().MustStoreUInt(0x30, 8).MustStoreRef(leafC).EndCell()

	a := BeginCell().MustStoreUInt(0x01, 8).MustStoreRef(sharedA).MustStoreRef(sharedA).EndCell()
	b := BeginCell().MustStoreUInt(0x01, 8).MustStoreRef(sharedB).MustStoreRef(sharedA).EndCell()
	c := BeginCell().MustStoreUInt(0x01, 8).MustStoreRef(sharedB).MustStoreRef(sharedC).EndCell()

	updateABFrom, updateABTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(a, b)
	if err != nil {
		t.Fatalf("failed to build AB update bodies: %v", err)
	}
	updateBCFrom, updateBCTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(b, c)
	if err != nil {
		t.Fatalf("failed to build BC update bodies: %v", err)
	}

	combined, err := CombineMerkleUpdate(
		mustMerkleUpdateCell(t, updateABFrom, updateABTo),
		mustMerkleUpdateCell(t, updateBCFrom, updateBCTo),
	)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	checkMerkleUpdate(t, a, c, combined)
}

func TestCombineMerkleUpdateArrayLikeChain(t *testing.T) {
	const n = 64

	baseValues := make([]uint16, n)
	for i := range baseValues {
		baseValues[i] = uint16(i / 3)
	}

	initialValues := append([]uint16{}, baseValues...)
	currentValues := append([]uint16{}, baseValues...)
	initialRoot := buildBinaryTree(initialValues)
	currentRoot := initialRoot
	var updates []*Cell

	applyOp := func(op func([]uint16)) {
		nextValues := append([]uint16{}, currentValues...)
		op(nextValues)

		nextRoot := buildBinaryTree(nextValues)
		updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(currentRoot, nextRoot)
		if err != nil {
			t.Fatalf("failed to build update bodies: %v", err)
		}
		updates = append(updates, mustMerkleUpdateCell(t, updateFrom, updateTo))
		currentValues = nextValues
		currentRoot = nextRoot
	}

	validateCombined := func() {
		if len(updates) == 0 {
			t.Fatal("expected at least one update")
		}
		for len(updates) > 1 {
			combined, err := CombineMerkleUpdate(updates[len(updates)-2], updates[len(updates)-1])
			if err != nil {
				t.Fatalf("combine failed: %v", err)
			}
			updates[len(updates)-2] = combined
			updates = updates[:len(updates)-1]
		}
		checkMerkleUpdate(t, initialRoot, currentRoot, updates[0])
	}

	applyOp(func([]uint16) {})
	validateCombined()

	applyOp(func([]uint16) {})
	applyOp(func([]uint16) {})
	applyOp(func([]uint16) {})
	validateCombined()

	applyOp(func(values []uint16) {
		for i := range values {
			values[i] = uint16(i/3 + 10)
		}
	})
	applyOp(func(values []uint16) {
		for i := range values {
			values[i] = uint16(i / 3)
		}
	})
	validateCombined()

	for i := 0; i+1 < n; i++ {
		idx := i
		applyOp(func(values []uint16) {
			values[idx] = uint16(idx/3 + 1)
			if idx != 0 {
				values[idx-1] = uint16((idx - 1) / 3)
			}
		})
	}
	validateCombined()
}

// prunedBranchWithStoredMeta builds a level-1 pruned branch that claims the
// given hash/depth pair, without deriving them from a real cell.
func prunedBranchWithStoredMeta(tb testing.TB, hash []byte, depth uint16) *Cell {
	tb.Helper()

	data := make([]byte, 2+hashSize+depthSize)
	data[0] = byte(PrunedCellType)
	data[1] = 1
	copy(data[2:], hash)
	binary.BigEndian.PutUint16(data[2+hashSize:], depth)

	pruned := &Cell{
		bitsSz: uint16(len(data) * 8),
		data:   data,
	}
	pruned.setSpecial(true)
	pruned.setLevelMask(LevelMask{Mask: 1})
	if err := validateBoundaryCell(pruned); err != nil {
		tb.Fatalf("failed to validate handmade pruned branch: %v", err)
	}
	if err := pruned.calculateHashes(); err != nil {
		tb.Fatalf("failed to finalize handmade pruned branch: %v", err)
	}
	return pruned
}

func TestMerkleUpdateRejectsBoundaryDepthMismatch(t *testing.T) {
	// RW-13: the destination boundary reuses the source leaf hash but claims
	// depth 1 instead of the real depth 0. The reference compare_cells checks
	// hash AND depth on every level, so validation and apply must reject it.
	leaf := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	updateFrom := BeginCell().MustStoreUInt(0x01, 8).MustStoreRef(leaf).EndCell()

	badPruned := prunedBranchWithStoredMeta(t, leaf.getHash(0), leaf.getDepth(0)+1)
	badUpdateTo := BeginCell().MustStoreUInt(0x02, 8).MustStoreRef(badPruned).EndCell()
	badUpdate := mustMerkleUpdateCell(t, updateFrom, badUpdateTo)

	if err := ValidateMerkleUpdate(badUpdate); err == nil {
		t.Fatal("expected validate to reject a boundary with mismatched stored depth")
	}
	if _, err := ApplyMerkleUpdate(updateFrom, badUpdate); err == nil {
		t.Fatal("expected apply to reject a boundary with mismatched stored depth")
	}

	// control: the same boundary with the correct stored depth passes
	goodPruned := prunedBranchWithStoredMeta(t, leaf.getHash(0), leaf.getDepth(0))
	goodUpdateTo := BeginCell().MustStoreUInt(0x02, 8).MustStoreRef(goodPruned).EndCell()
	goodUpdate := mustMerkleUpdateCell(t, updateFrom, goodUpdateTo)

	if err := ValidateMerkleUpdate(goodUpdate); err != nil {
		t.Fatalf("validate failed for correct boundary: %v", err)
	}
	expected := BeginCell().MustStoreUInt(0x02, 8).MustStoreRef(leaf).EndCell()
	got, err := ApplyMerkleUpdate(updateFrom, goodUpdate)
	if err != nil {
		t.Fatalf("apply failed for correct boundary: %v", err)
	}
	assertCellsEqual(t, got, expected)
}

func TestMerkleUpdateRejectsNonZeroLevelRoot(t *testing.T) {
	leafA := BeginCell().MustStoreUInt(0xA, 4).EndCell()
	leafB := BeginCell().MustStoreUInt(0xB, 4).EndCell()
	branchA := BeginCell().MustStoreRef(leafA).EndCell()
	branchB := BeginCell().MustStoreRef(leafB).EndCell()

	prunedA, err := createPrunedBranchFromCell(branchA, 2)
	if err != nil {
		t.Fatalf("failed to create pruned A: %v", err)
	}
	prunedB, err := createPrunedBranchFromCell(branchB, 2)
	if err != nil {
		t.Fatalf("failed to create pruned B: %v", err)
	}
	update := mustMerkleUpdateCell(t, prunedA, prunedB)
	if update.Level() == 0 {
		t.Fatal("expected a non-zero-level merkle update root")
	}

	if err := ValidateMerkleUpdate(update); err == nil {
		t.Fatal("expected validate to reject a non-zero-level merkle update root")
	}
	if err := MayApplyMerkleUpdate(leafA, update); err == nil {
		t.Fatal("expected may apply to reject a non-zero-level merkle update root")
	}
	if _, err := ApplyMerkleUpdate(leafA, update); err == nil {
		t.Fatal("expected apply to reject a non-zero-level merkle update root")
	}
}

func TestCreateMerkleUpdateRejectsNonZeroLevelRoots(t *testing.T) {
	fromLeaf := BeginCell().MustStoreUInt(0xA, 4).EndCell()
	toLeaf := BeginCell().MustStoreUInt(0xB, 4).EndCell()
	fromBranch := BeginCell().MustStoreRef(fromLeaf).EndCell()
	toBranch := BeginCell().MustStoreRef(toLeaf).EndCell()

	fromPruned, err := createPrunedBranchFromCell(fromBranch, 1)
	if err != nil {
		t.Fatalf("failed to create source pruned branch: %v", err)
	}
	toPruned, err := createPrunedBranchFromCell(toBranch, 1)
	if err != nil {
		t.Fatalf("failed to create destination pruned branch: %v", err)
	}

	from := BeginCell().MustStoreUInt(0, 1).MustStoreRef(fromPruned).EndCell()
	to := BeginCell().MustStoreUInt(1, 1).MustStoreRef(toPruned).EndCell()
	if from.Level() == 0 || to.Level() == 0 {
		t.Fatal("expected non-zero-level roots")
	}

	rs := NewReadSet(from)
	if _, err = rs.CreateMerkleUpdate(to); err == nil {
		t.Fatal("expected merkle update generation to reject non-zero-level roots")
	}
	if _, _, _, _, err = rs.createMerkleUpdateRaw(to, false, 0, 1); err == nil {
		t.Fatal("expected raw merkle update generation to reject non-zero-level roots")
	}
}

func buildBinaryTree(values []uint16) *Cell {
	if len(values) == 1 {
		return BeginCell().MustStoreUInt(uint64(values[0]), 16).EndCell()
	}

	mid := len(values) / 2
	left := buildBinaryTree(values[:mid])
	right := buildBinaryTree(values[mid:])

	return BeginCell().
		MustStoreUInt(uint64(len(values)), 8).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()
}

func buildOrdinaryMerkleUpdateBodies(from, to *Cell, path []int) (*Cell, *Cell, error) {
	if from == nil || to == nil {
		return nil, nil, nil
	}
	if len(path) == 0 {
		return from, to, nil
	}

	if from.refsCount() != 2 || to.refsCount() != 2 {
		return nil, nil, bytes.ErrTooLarge
	}

	refsFrom := make([]*Cell, 2)
	refsTo := make([]*Cell, 2)
	for i := 0; i < 2; i++ {
		if i == path[0] {
			nextFrom, nextTo, err := buildOrdinaryMerkleUpdateBodies(from.ref(i), to.ref(i), path[1:])
			if err != nil {
				return nil, nil, err
			}
			refsFrom[i] = nextFrom
			refsTo[i] = nextTo
			continue
		}

		prunedFrom, err := createPrunedBranchFromCell(from.ref(i), 1)
		if err != nil {
			return nil, nil, err
		}
		prunedTo, err := createPrunedBranchFromCell(to.ref(i), 1)
		if err != nil {
			return nil, nil, err
		}
		refsFrom[i] = prunedFrom
		refsTo[i] = prunedTo
	}

	updateFrom, err := copyCellWithRefs(from, refsFrom)
	if err != nil {
		return nil, nil, err
	}
	updateTo, err := copyCellWithRefs(to, refsTo)
	if err != nil {
		return nil, nil, err
	}
	return updateFrom, updateTo, nil
}

func buildOrdinaryMerkleUpdateBodiesFromDiff(from, to *Cell) (*Cell, *Cell, error) {
	return buildOrdinaryMerkleUpdateBodiesFromDiffAt(from, to, false)
}

func buildOrdinaryMerkleUpdateBodiesFromDiffAt(from, to *Cell, allowPrune bool) (*Cell, *Cell, error) {
	if from == nil || to == nil {
		return nil, nil, bytes.ErrTooLarge
	}
	if from.HashKey() == to.HashKey() {
		if !allowPrune {
			return from, to, nil
		}
		prunedFrom, err := pruneOrdinaryUpdateSubtree(from)
		if err != nil {
			return nil, nil, err
		}
		prunedTo, err := pruneOrdinaryUpdateSubtree(to)
		if err != nil {
			return nil, nil, err
		}
		return prunedFrom, prunedTo, nil
	}
	if from.refsCount() != to.refsCount() || from.IsSpecial() != to.IsSpecial() {
		return nil, nil, bytes.ErrTooLarge
	}
	if from.refsCount() == 0 {
		return from, to, nil
	}

	refsFrom := make([]*Cell, from.refsCount())
	refsTo := make([]*Cell, to.refsCount())
	for i := 0; i < len(refsFrom); i++ {
		nextFrom, nextTo, err := buildOrdinaryMerkleUpdateBodiesFromDiffAt(from.ref(i), to.ref(i), true)
		if err != nil {
			return nil, nil, err
		}
		refsFrom[i] = nextFrom
		refsTo[i] = nextTo
	}

	updateFrom, err := copyCellWithRefs(from, refsFrom)
	if err != nil {
		return nil, nil, err
	}
	updateTo, err := copyCellWithRefs(to, refsTo)
	if err != nil {
		return nil, nil, err
	}
	return updateFrom, updateTo, nil
}

func pruneOrdinaryUpdateSubtree(cell *Cell) (*Cell, error) {
	if cell == nil {
		return nil, bytes.ErrTooLarge
	}
	if cell.refsCount() == 0 {
		return cell, nil
	}
	return createPrunedBranchFromCell(cell, 1)
}

func binaryPath(index, depth int) []int {
	path := make([]int, depth)
	for i := 0; i < depth; i++ {
		shift := depth - i - 1
		path[i] = (index >> shift) & 1
	}
	return path
}

func binaryPathName(index, depth int) string {
	path := binaryPath(index, depth)
	name := make([]byte, len(path))
	for i, bit := range path {
		name[i] = byte('0' + bit)
	}
	return string(name)
}

func checkMerkleUpdate(tb testing.TB, from, to, update *Cell) {
	tb.Helper()

	if err := MayApplyMerkleUpdate(from, update); err != nil {
		tb.Fatalf("may apply failed: %v", err)
	}
	if err := ValidateMerkleUpdate(update); err != nil {
		tb.Fatalf("validate failed: %v", err)
	}

	got, err := ApplyMerkleUpdate(from, update)
	if err != nil {
		tb.Fatalf("apply failed: %v", err)
	}
	assertCellsEqual(tb, got, to)
}

func randomLeafValues(rnd *rand.Rand, count int) []uint16 {
	values := make([]uint16, count)
	for i := range values {
		values[i] = uint16(rnd.Intn(1 << 15))
	}
	return values
}

func mutateLeafValues(rnd *rand.Rand, values []uint16, changes int) []uint16 {
	next := append([]uint16{}, values...)
	if len(next) == 0 {
		return next
	}
	if changes < 1 {
		changes = 1
	}

	used := map[int]struct{}{}
	for len(used) < changes {
		idx := rnd.Intn(len(next))
		if _, ok := used[idx]; ok {
			continue
		}
		used[idx] = struct{}{}

		updated := next[idx]
		for updated == next[idx] {
			updated = uint16(rnd.Intn(1 << 15))
		}
		next[idx] = updated
	}
	return next
}

func assertCellsEqual(tb testing.TB, got, want *Cell) {
	tb.Helper()

	if got.HashKey() != want.HashKey() {
		tb.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), want.Hash())
	}
	if hasPrunedCells(got, map[Hash]struct{}{}) {
		return
	}
	if !bytes.Equal(got.ToBOC(), want.ToBOC()) {
		tb.Fatalf("boc mismatch:\n got: %x\nwant: %x", got.ToBOC(), want.ToBOC())
	}
}

func hasPrunedCells(cl *Cell, visited map[Hash]struct{}) bool {
	if cl == nil {
		return false
	}
	hash := cl.HashKey()
	if _, ok := visited[hash]; ok {
		return false
	}
	visited[hash] = struct{}{}

	if cl.GetType() == PrunedCellType {
		return true
	}
	for i := 0; i < cl.refsCount(); i++ {
		if hasPrunedCells(cl.ref(i), visited) {
			return true
		}
	}
	return false
}

func copyCellWithRefs(src *Cell, refs []*Cell) (*Cell, error) {
	builder := BeginCell()
	if err := storeBitSpan(builder, cellBits(src)); err != nil {
		return nil, err
	}
	for _, ref := range refs {
		if err := builder.StoreRef(ref); err != nil {
			return nil, err
		}
	}
	return finalizeCellFromBuilder(builder, src.IsSpecial())
}

func testLazyLoaderForCells(cells ...*Cell) *testLazyLoader {
	loader := &testLazyLoader{cells: map[Hash]*Cell{}}
	for _, cell := range cells {
		if cell == nil {
			continue
		}
		loader.cells[cell.HashKey()] = cell
		loader.cells[cell.HashKey(0)] = cell
		loader.cells[cell.HashKey(cell.Level())] = cell
	}
	return loader
}
