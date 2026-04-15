package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func mustLibraryCellForHash(t *testing.T, hash []byte) *cell.Cell {
	t.Helper()

	cl, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(hash, 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to build library cell: %v", err)
	}
	return cl
}

func mustPrunedCell(t *testing.T) *cell.Cell {
	t.Helper()

	branch := cell.BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		EndCell()
	root := cell.BeginCell().
		MustStoreUInt(0, 1).
		MustStoreRef(branch).
		EndCell()

	proof, err := root.CreateProof(cell.CreateProofSkeleton())
	if err != nil {
		t.Fatalf("failed to build proof for pruned cell: %v", err)
	}
	body, err := cell.UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("failed to unwrap proof for pruned cell: %v", err)
	}
	pruned, err := body.PeekRef(0)
	if err != nil {
		t.Fatalf("failed to get pruned ref: %v", err)
	}
	if !pruned.IsSpecial() || pruned.GetType() != cell.PrunedCellType {
		t.Fatalf("expected pruned cell, got special=%v type=%v", pruned.IsSpecial(), pruned.GetType())
	}
	return pruned
}
