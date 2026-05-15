package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func mustUsageProofWithLoadedRoot(t *testing.T, root *cell.Cell) *cell.Cell {
	t.Helper()

	builder := cell.NewMerkleProofBuilder(root)
	if _, err := builder.Root().BeginParse(); err != nil {
		t.Fatalf("trace proof root load: %v", err)
	}
	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatalf("create usage proof: %v", err)
	}
	return proof
}
