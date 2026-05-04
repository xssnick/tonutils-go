package cell

import (
	"bytes"
	"errors"
	"math/rand"
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

	got, reused, err := ApplyMerkleUpdate(node, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	assertCellsEqual(t, got, newNode)
	if len(reused.Cells) != 1 || reused.Cells[0].Hash != data.HashKey() || reused.Cells[0].Cell != data {
		t.Fatalf("unexpected reused cells: got=%x want=%x", reused, data.Hash())
	}
	if len(reused.Refs) != 1 || reused.Refs[0].LogicalHash != data.HashKey() || reused.Refs[0].RefIndex != 0 || reused.Refs[0].RawCell != reused.Cells[0].Cell {
		t.Fatalf("unexpected reused refs: %+v", reused.Refs)
	}

	if err := MayApplyMerkleUpdate(otherNode, update); err == nil {
		t.Fatal("expected may apply to reject a mismatched source root")
	}
	if _, _, err := ApplyMerkleUpdate(otherNode, update); err == nil {
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
	if _, _, err := ApplyMerkleUpdate(otherNode, otherUpdate); err == nil {
		t.Fatal("expected apply to reject unknown pruned destination branches")
	}
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

			got, _, err := ApplyMerkleUpdate(from, update)
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

	got, reused, err := ApplyMerkleUpdate(from, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	assertCellsEqual(t, got, to)
	if !containsMerkleUpdateReusedCell(reused.Cells, from.ref(1).HashKey()) {
		t.Fatalf("expected unchanged right subtree in reused cells: %x", from.ref(1).Hash())
	}
	if got.ref(1) != from.ref(1) {
		t.Fatal("expected unchanged right subtree to reuse source reference identity")
	}
}

func TestApplyMerkleUpdatePreservesUnchangedLazyRefs(t *testing.T) {
	from := buildBinaryTree([]uint16{1, 2, 3, 4})
	to := buildBinaryTree([]uint16{101, 2, 3, 4})

	updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodiesFromDiff(from, to)
	if err != nil {
		t.Fatalf("failed to build merkle update bodies: %v", err)
	}
	update := mustMerkleUpdateCell(t, updateFrom, updateTo)

	loader := testLazyLoaderForCells(from.rawRefs()...)
	lazyFrom := cellWithLazyRefsFromCell(from, loader.LoadCell)

	got, reused, err := ApplyMerkleUpdate(lazyFrom, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if got.HashKey() != to.HashKey() {
		t.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), to.Hash())
	}
	rightHash := from.ref(1).HashKey()
	if !containsMerkleUpdateReusedCell(reused.Cells, rightHash) {
		t.Fatalf("expected unchanged right subtree in reused cells: %x", rightHash)
	}
	if len(reused.Cells) != 1 || reused.Cells[0].Cell == nil || reused.Cells[0].Cell.GetType() != PrunedCellType || !reused.Cells[0].Cell.IsLazy() {
		t.Fatalf("expected reused cell metadata to carry source lazy ref identity, got %+v", reused)
	}
	if len(reused.Refs) != 1 || reused.Refs[0].LogicalHash != rightHash || reused.Refs[0].RefIndex != 1 || reused.Refs[0].RawCell != reused.Cells[0].Cell {
		t.Fatalf("unexpected reused ref metadata: %+v", reused.Refs)
	}
	if loader.calls != 1 {
		t.Fatalf("expected only changed source path to be loaded during apply, got %d", loader.calls)
	}

	right, err := got.PeekRef(1)
	if err != nil {
		t.Fatalf("load unchanged right ref: %v", err)
	}
	if right.HashKey() != from.ref(1).HashKey() {
		t.Fatalf("unexpected right subtree hash: got=%x want=%x", right.Hash(), from.ref(1).Hash())
	}
}

func TestApplyMerkleUpdateFullSourceWalkLoadsLazyBoundaries(t *testing.T) {
	from := buildBinaryTree([]uint16{1, 2, 3, 4})

	prunedLeft, err := createPrunedBranchFromCell(from.ref(0), 1)
	if err != nil {
		t.Fatalf("failed to prune left subtree: %v", err)
	}
	prunedRight, err := createPrunedBranchFromCell(from.ref(1), 1)
	if err != nil {
		t.Fatalf("failed to prune right subtree: %v", err)
	}
	updateTo, err := copyCellWithRefs(from, []*Cell{prunedLeft, prunedRight})
	if err != nil {
		t.Fatalf("failed to build pruned destination: %v", err)
	}
	update := mustMerkleUpdateCell(t, from, updateTo)

	loader := testLazyLoaderForCells(from.rawRefs()...)
	lazyFrom := cellWithLazyRefsFromCell(from, loader.LoadCell)

	got, reused, err := ApplyMerkleUpdate(lazyFrom, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if got.HashKey() != from.HashKey() {
		t.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), from.Hash())
	}
	if loader.calls != 2 {
		t.Fatalf("expected full source walk to load both lazy boundary refs, got %d", loader.calls)
	}

	leftHash := from.ref(0).HashKey()
	rightHash := from.ref(1).HashKey()
	if !containsMerkleUpdateReusedCell(reused.Cells, leftHash) || !containsMerkleUpdateReusedCell(reused.Cells, rightHash) {
		t.Fatalf("expected both child subtrees in reused cells: %+v", reused.Cells)
	}
	if got.ref(0).IsLazy() || got.ref(1).IsLazy() {
		t.Fatalf("expected full source walk to materialize reused refs")
	}
}

func TestApplyMerkleUpdateCollectsKnownBranchesWhenDestinationShapeDiffers(t *testing.T) {
	left := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	right := BeginCell().MustStoreUInt(0xBB, 8).EndCell()
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

	got, reused, err := ApplyMerkleUpdate(lazyFrom, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if got.HashKey() != to.HashKey() {
		t.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), to.Hash())
	}
	if !containsMerkleUpdateReusedCell(reused.Cells, left.HashKey()) {
		t.Fatalf("expected reused left subtree in reused cells: %+v", reused.Cells)
	}
	if !got.ref(0).IsLazy() {
		t.Fatal("expected reused left ref to remain lazy")
	}
}

func TestApplyMerkleUpdateCollectsMovedDestinationBoundary(t *testing.T) {
	reused := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	oldLeft := BeginCell().MustStoreUInt(0x10, 8).MustStoreRef(reused).EndCell()
	oldRight := BeginCell().MustStoreUInt(0x20, 8).EndCell()
	from := BeginCell().
		MustStoreUInt(0x30, 8).
		MustStoreRef(oldLeft).
		MustStoreRef(oldRight).
		EndCell()

	prunedReused, err := createPrunedBranchFromCell(reused, 1)
	if err != nil {
		t.Fatalf("failed to prune reused subtree: %v", err)
	}
	updateTo := BeginCell().
		MustStoreUInt(0x40, 8).
		MustStoreRef(prunedReused).
		MustStoreRef(BeginCell().MustStoreUInt(0xBB, 8).EndCell()).
		EndCell()
	update := mustMerkleUpdateCell(t, from, updateTo)

	loader := testLazyLoaderForCells(from.rawRefs()...)
	lazyFrom := cellWithLazyRefsFromCell(from, loader.LoadCell)

	got, reusedMeta, err := ApplyMerkleUpdate(lazyFrom, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if got.HashKey(0) != updateTo.HashKey(0) {
		t.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), updateTo.Hash())
	}
	if !containsMerkleUpdateReusedCell(reusedMeta.Cells, reused.HashKey()) {
		t.Fatalf("expected moved reused subtree in reused cells: %+v", reusedMeta.Cells)
	}
	if loader.calls == 0 {
		t.Fatal("expected fallback source walk to load old subtree")
	}
}

func TestApplyMerkleUpdateFallsBackWhenBoundaryIsInsideSkippedSubtree(t *testing.T) {
	oldLeft := BeginCell().MustStoreUInt(0x10, 8).EndCell()
	reused := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	oldRight := BeginCell().MustStoreUInt(0x20, 8).MustStoreRef(reused).EndCell()
	from := BeginCell().
		MustStoreUInt(0x30, 8).
		MustStoreRef(oldLeft).
		MustStoreRef(oldRight).
		EndCell()

	prunedReused, err := createPrunedBranchFromCell(reused, 1)
	if err != nil {
		t.Fatalf("failed to prune reused subtree: %v", err)
	}
	prunedOldRight, err := createPrunedBranchFromCell(oldRight, 1)
	if err != nil {
		t.Fatalf("failed to prune old right subtree: %v", err)
	}
	updateTo := BeginCell().
		MustStoreUInt(0x40, 8).
		MustStoreRef(prunedReused).
		MustStoreRef(prunedOldRight).
		EndCell()
	update := mustMerkleUpdateCell(t, from, updateTo)

	loader := testLazyLoaderForCells(from.rawRefs()...)
	lazyFrom := cellWithLazyRefsFromCell(from, loader.LoadCell)

	got, reusedMeta, err := ApplyMerkleUpdate(lazyFrom, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if got.HashKey(0) != updateTo.HashKey(0) {
		t.Fatalf("hash mismatch: got=%x want=%x", got.Hash(), updateTo.Hash())
	}
	if !containsMerkleUpdateReusedCell(reusedMeta.Cells, reused.HashKey()) {
		t.Fatalf("expected nested moved subtree in reused cells: %+v", reusedMeta.Cells)
	}
	if loader.calls == 0 {
		t.Fatal("expected fallback source walk to load skipped old subtree")
	}
}

func containsMerkleUpdateReusedCell(cells []MerkleUpdateReusedCell, hash Hash) bool {
	for _, candidate := range cells {
		if candidate.Hash == hash {
			return true
		}
	}
	return false
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

func TestMerkleUpdateRejectsNonZeroLevelRoot(t *testing.T) {
	leafA := BeginCell().MustStoreUInt(0xA, 4).EndCell()
	leafB := BeginCell().MustStoreUInt(0xB, 4).EndCell()

	prunedA, err := createPrunedBranchFromCell(leafA, 2)
	if err != nil {
		t.Fatalf("failed to create pruned A: %v", err)
	}
	prunedB, err := createPrunedBranchFromCell(leafB, 2)
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
	if _, _, err := ApplyMerkleUpdate(leafA, update); err == nil {
		t.Fatal("expected apply to reject a non-zero-level merkle update root")
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

	got, _, err := ApplyMerkleUpdate(from, update)
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
