package cell

import (
	"bytes"
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

	got, err := ApplyMerkleUpdate(node, update)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	assertCellsEqual(t, got, newNode)

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
		t.Fatal("expected unchanged right subtree to be reused from source tree")
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
	if _, err := ApplyMerkleUpdate(leafA, update); err == nil {
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
	if from.refsCount() != to.refsCount() || from.isSpecial() != to.isSpecial() {
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
	if !bytes.Equal(got.ToBOC(), want.ToBOC()) {
		tb.Fatalf("boc mismatch:\n got: %x\nwant: %x", got.ToBOC(), want.ToBOC())
	}
}
