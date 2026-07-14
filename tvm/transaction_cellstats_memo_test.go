package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// transactionCellStatsReference is the pre-memoization walker: it re-walks
// children of already-seen cells and tracks the merkle depth accumulated along
// each path. The memoized walker must match it exactly.
func transactionCellStatsReference(t *testing.T, roots ...*cell.Cell) transactionCellStatsResult {
	t.Helper()

	var stats transactionCellStatsResult
	seenUsage := make(map[cell.Hash]struct{})

	var walk func(c *cell.Cell, depth uint16)
	walk = func(c *cell.Cell, depth uint16) {
		if c == nil {
			return
		}

		var sl cell.Slice
		if err := c.BeginParseIntoWithoutTrace(&sl); err != nil {
			t.Fatalf("failed to parse cell: %v", err)
		}

		loaded := sl.BaseCell()
		switch loaded.GetType() {
		case cell.MerkleProofCellType, cell.MerkleUpdateCellType:
			depth++
			if depth > stats.merkleDepth {
				stats.merkleDepth = depth
			}
		}

		key := loaded.HashKey()
		if _, ok := seenUsage[key]; !ok {
			seenUsage[key] = struct{}{}
			stats.usage.cells++
			stats.usage.bits += uint64(loaded.BitsSize())
		}

		for sl.RefsNum() > 0 {
			ref, err := sl.LoadRefCell()
			if err != nil {
				t.Fatalf("failed to load ref: %v", err)
			}
			walk(ref, depth)
		}
	}
	for _, root := range roots {
		walk(root, 0)
	}
	return stats
}

func assertCellStatsMatchReference(t *testing.T, name string, roots ...*cell.Cell) {
	t.Helper()

	want := transactionCellStatsReference(t, roots...)
	got, err := transactionCellStatsForRoots(roots...)
	if err != nil {
		t.Fatalf("%s: transactionCellStatsForRoots failed: %v", name, err)
	}
	if got != want {
		t.Fatalf("%s: stats mismatch: got %+v want %+v", name, got, want)
	}
}

func TestTransactionCellStatsMemoMatchesReference(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	proof, err := cell.CreateMerkleProof(leaf)
	if err != nil {
		t.Fatalf("failed to create merkle proof: %v", err)
	}
	nestedProof, err := cell.CreateMerkleProof(proof)
	if err != nil {
		t.Fatalf("failed to create nested merkle proof: %v", err)
	}

	// The proof cell is shared: once directly under the root (merkle path
	// prefix 0) and once inside the nested proof (prefix 1). The memoized
	// subtree depth must stay path-independent, so the deeper path still
	// dominates the result.
	sharedRoot := cell.BeginCell().
		MustStoreUInt(1, 8).
		MustStoreRef(nestedProof).
		MustStoreRef(proof).
		EndCell()
	assertCellStatsMatchReference(t, "shared proof via two merkle prefixes", sharedRoot)

	// Reversed ref order: the shared cell is first reached via the shallow
	// path and memoized there, then contributes to the deep path.
	sharedRootReversed := cell.BeginCell().
		MustStoreUInt(2, 8).
		MustStoreRef(proof).
		MustStoreRef(nestedProof).
		EndCell()
	assertCellStatsMatchReference(t, "shared proof shallow path first", sharedRootReversed)

	// Ordinary diamond sharing without merkle cells.
	shared := cell.BeginCell().MustStoreUInt(7, 16).MustStoreRef(leaf).EndCell()
	left := cell.BeginCell().MustStoreUInt(1, 4).MustStoreRef(shared).EndCell()
	right := cell.BeginCell().MustStoreUInt(2, 4).MustStoreRef(shared).EndCell()
	diamond := cell.BeginCell().MustStoreRef(left).MustStoreRef(right).EndCell()
	assertCellStatsMatchReference(t, "ordinary diamond", diamond)

	// Multiple roots including duplicates and shared subtrees.
	assertCellStatsMatchReference(t, "multiple roots", sharedRoot, sharedRoot, nestedProof, diamond, proof)

	// Plain no-merkle single root.
	assertCellStatsMatchReference(t, "plain root", leaf)
}
