package tlb

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func makeBinTreeLeaf(value *cell.Cell) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0, 1).
		MustStoreBuilder(value.ToBuilder()).
		EndCell()
}

func makeBinTreeFork(left, right *cell.Cell) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(1, 1).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()
}

func TestBinTreeWalkAndGet(t *testing.T) {
	leftValue := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	rightValue := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	root := makeBinTreeFork(makeBinTreeLeaf(leftValue), makeBinTreeLeaf(rightValue))

	var tree BinTree
	if err := LoadFromCell(&tree, root.BeginParse()); err != nil {
		t.Fatal(err)
	}

	leftKey := cell.BeginCell().MustStoreUInt(0, 1).EndCell()
	rightKey := cell.BeginCell().MustStoreUInt(1, 1).EndCell()

	if got := tree.Get(leftKey); got == nil || got.HashKey() != leftValue.HashKey() {
		t.Fatal("left leaf lookup failed")
	}
	if got := tree.Get(rightKey); got == nil || got.HashKey() != rightValue.HashKey() {
		t.Fatal("right leaf lookup failed")
	}
	if got := tree.Get(cell.BeginCell().EndCell()); got != nil {
		t.Fatal("empty key should not match a fork")
	}
	if got := tree.Get(cell.BeginCell().MustStoreUInt(0, 1).MustStoreRef(cell.BeginCell().EndCell()).EndCell()); got != nil {
		t.Fatal("key with refs should not match")
	}
	if got := tree.Get(cell.BeginCell().MustStoreUInt(2, 2).EndCell()); got != nil {
		t.Fatal("long key should not match a leaf")
	}

	seen := map[cell.Hash]cell.Hash{}
	if err := tree.Walk(func(key *cell.Cell, value *cell.Cell) error {
		seen[key.HashKey()] = value.HashKey()
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 2 {
		t.Fatalf("unexpected number of leaves: %d", len(seen))
	}
	if seen[leftKey.HashKey()] != leftValue.HashKey() {
		t.Fatal("left leaf was not visited correctly")
	}
	if seen[rightKey.HashKey()] != rightValue.HashKey() {
		t.Fatal("right leaf was not visited correctly")
	}
}

func TestBinTreePrunedLeafCompatibility(t *testing.T) {
	leftValue := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	rightBranch := makeBinTreeFork(
		makeBinTreeLeaf(cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()),
		makeBinTreeLeaf(cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()),
	)
	root := makeBinTreeFork(makeBinTreeLeaf(leftValue), rightBranch)

	sk := cell.CreateProofSkeleton()
	sk.ProofRef(0).SetRecursive()

	proof, err := root.CreateProof(sk)
	if err != nil {
		t.Fatal(err)
	}
	prunedRoot, err := cell.UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatal(err)
	}

	var tree BinTree
	if err = LoadFromCell(&tree, prunedRoot.BeginParse()); err != nil {
		t.Fatal(err)
	}

	leftKey := cell.BeginCell().MustStoreUInt(0, 1).EndCell()
	rightKey := cell.BeginCell().MustStoreUInt(1, 1).EndCell()

	if got := tree.Get(leftKey); got == nil || got.HashKey() != leftValue.HashKey() {
		t.Fatal("ordinary proven leaf should be exposed")
	}
	if got := tree.Get(rightKey); got == nil || got.GetType() != cell.PrunedCellType {
		t.Fatal("pruned proof branch should be exposed as an opaque leaf")
	}

	all := tree.All()
	if len(all) != 2 {
		t.Fatalf("unexpected number of leaves: %d", len(all))
	}

	var seenPruned bool
	for _, item := range all {
		if item.Key.HashKey() == rightKey.HashKey() && item.Value.GetType() == cell.PrunedCellType {
			seenPruned = true
		}
	}
	if !seenPruned {
		t.Fatal("pruned branch was not preserved in walk/all")
	}
}

func TestBinTreeMalformedForkDoesNotPanic(t *testing.T) {
	malformed := cell.BeginCell().
		MustStoreUInt(1, 1).
		MustStoreRef(makeBinTreeLeaf(cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell())).
		EndCell()

	var tree BinTree
	if err := LoadFromCell(&tree, malformed.BeginParse()); err != nil {
		t.Fatal(err)
	}

	if got := tree.Get(cell.BeginCell().MustStoreUInt(0, 1).EndCell()); got != nil {
		t.Fatal("malformed fork should not resolve to a value")
	}
	if err := tree.Walk(func(_ *cell.Cell, _ *cell.Cell) error { return nil }); err == nil {
		t.Fatal("malformed fork should be rejected during walk")
	}
}
