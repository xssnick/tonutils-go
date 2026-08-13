package cell

import (
	"bytes"
	"testing"
)

func TestBuildMerkleProofBodyPruneCallbackMemoizedBeforeSharedDAGVisit(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	shared := BeginCell().MustStoreUInt(0xCDEF, 16).MustStoreRef(leaf).EndCell()
	root := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(shared).MustStoreRef(shared).EndCell()

	sharedCalls := 0
	built, err := buildMerkleProofBodyByPruneFunc(root, func(c *Cell, _ int, _ Hash) (*Cell, bool, error) {
		if c.HashKey() != shared.HashKey() {
			return nil, false, nil
		}
		sharedCalls++
		// Deliberately path-dependent: without the C++ pre-callback memo lookup,
		// the second edge would include the full subtree instead of reusing the
		// first edge's pruned result.
		return nil, sharedCalls == 1, nil
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sharedCalls != 1 {
		t.Fatalf("shared cell callback count: got %d want 1", sharedCalls)
	}

	pruned, err := createPrunedBranchFromCell(shared, 1)
	if err != nil {
		t.Fatal(err)
	}
	expected := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(pruned).MustStoreRef(pruned).EndCell()
	if !bytes.Equal(built.ToBOCWithOptions(BOCSerializeOptions{}), expected.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatalf("shared-DAG proof body mismatch:\n got: %x\nwant: %x",
			built.ToBOCWithOptions(BOCSerializeOptions{}), expected.ToBOCWithOptions(BOCSerializeOptions{}))
	}
	if built.ref(0) != built.ref(1) {
		t.Fatal("shared-DAG proof did not reuse the memoized cell identity")
	}
}

func TestBuildMerkleProofBodyMemoKeyIncludesMerkleDepth(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	shared := BeginCell().MustStoreUInt(0xCDEF, 16).MustStoreRef(leaf).EndCell()
	nestedProof, err := CreateMerkleProof(shared)
	if err != nil {
		t.Fatal(err)
	}
	root := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(shared).MustStoreRef(nestedProof).EndCell()

	sharedCalls := 0
	built, err := buildMerkleProofBodyByPruneFunc(root, func(c *Cell, _ int, _ Hash) (*Cell, bool, error) {
		if c.HashKey() == shared.HashKey() {
			sharedCalls++
			return nil, true, nil
		}
		return nil, false, nil
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sharedCalls != 2 {
		t.Fatalf("shared cell at two Merkle depths callback count: got %d want 2", sharedCalls)
	}

	directPruned, err := createPrunedBranchFromCell(shared, 1)
	if err != nil {
		t.Fatal(err)
	}
	nestedPruned, err := createPrunedBranchFromCell(shared, 2)
	if err != nil {
		t.Fatal(err)
	}
	expectedNestedProof, err := CreateMerkleProof(nestedPruned)
	if err != nil {
		t.Fatal(err)
	}
	expected := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(directPruned).MustStoreRef(expectedNestedProof).EndCell()
	if !bytes.Equal(built.ToBOCWithOptions(BOCSerializeOptions{}), expected.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatalf("depth-sensitive proof body mismatch:\n got: %x\nwant: %x",
			built.ToBOCWithOptions(BOCSerializeOptions{}), expected.ToBOCWithOptions(BOCSerializeOptions{}))
	}
}

func BenchmarkBuildMerkleProofBodySharedDAGMemo(b *testing.B) {
	for _, test := range []struct {
		name  string
		depth int
	}{
		{name: "inline_depth_4", depth: 4},
		{name: "map_depth_16", depth: 16},
	} {
		b.Run(test.name, func(b *testing.B) {
			root := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
			for i := 0; i < test.depth; i++ {
				root = BeginCell().MustStoreUInt(uint64(i), 8).MustStoreRef(root).MustStoreRef(root).EndCell()
			}

			b.ReportAllocs()
			for b.Loop() {
				built, err := buildMerkleProofBodyByPruneFunc(root, nil, 0)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkCellSink = built
			}
		})
	}
}
