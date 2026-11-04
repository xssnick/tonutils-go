package cell

import (
	"fmt"
	"testing"
)

func TestUsageProofLazyMerkleParentPreservesChildLevel(t *testing.T) {
	hidden := BeginCell().MustStoreRef(BeginCell().MustStoreUInt(0x11, 8).EndCell()).EndCell()
	pruned, err := CreatePrunedBranch(hidden, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	left := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(pruned).EndCell()
	right := BeginCell().MustStoreUInt(0x33, 8).MustStoreRef(pruned).EndCell()
	if left.Level() != 1 || right.Level() != 1 {
		t.Fatal("test requires level-1 Merkle children")
	}

	for _, typ := range []Type{MerkleProofCellType, MerkleUpdateCellType} {
		var nested *Cell
		if typ == MerkleProofCellType {
			nested, err = CreateMerkleProof(left)
		} else {
			nested, err = CreateMerkleUpdate(left, right)
		}
		if err != nil {
			t.Fatal(err)
		}
		root := BeginCell().MustStoreRef(nested).EndCell()
		boc := root.ToBOC()
		for _, trusted := range []bool{false, true} {
			for _, parallel := range []bool{false, true} {
				t.Run(fmt.Sprintf("type%d/trusted%t/parallel%t", typ, trusted, parallel), func(t *testing.T) {
					lazy, err := FromBOCWithOptions(boc, BOCParseOptions{Lazy: true, TrustedHashes: trusted})
					if err != nil {
						t.Fatal(err)
					}
					read := NewReadSet(lazy)
					rootSlice, err := read.Root().BeginParse()
					if err != nil {
						t.Fatal(err)
					}
					parent, err := rootSlice.PeekRefCellAt(0)
					if err != nil {
						t.Fatal(err)
					}
					if !parent.IsLazy() {
						t.Fatal("test requires a lazy Merkle parent")
					}
					parentSlice, err := parent.BeginParse()
					if err != nil {
						t.Fatal(err)
					}
					for i := 0; i < int(parentSlice.RefsNum()); i++ {
						child, err := parentSlice.PeekRefCellAt(i)
						if err != nil {
							t.Fatal(err)
						}
						if _, err = child.BeginParse(); err != nil {
							t.Fatal(err)
						}
					}

					var proof *Cell
					if parallel {
						proof, err = lazy.CreateHashUsageProofResolvedSizedParallel(func(hash Hash) bool {
							_, loaded := read.Contains(hash)
							return loaded
						}, read.RecordedCell, proofParallelMinCells, 2)
					} else {
						proof, err = read.Proof()
					}
					if err != nil {
						t.Fatal(err)
					}
					decoded, err := FromBOC(proof.ToBOC())
					if err != nil {
						t.Fatalf("generated proof is not a valid BOC: %v", err)
					}
					if err = CheckProof(decoded, root.Hash(0)); err != nil {
						t.Fatalf("generated proof does not preserve the source root: %v", err)
					}
				})
			}
		}
	}
}
