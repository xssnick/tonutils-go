package cell

import "testing"

func TestCreateHashUsageProofSelectsCellsIndependentlyOfTracePath(t *testing.T) {
	target := BeginCell().MustStoreUInt(0xabc, 12).EndCell()
	keptBranch := BeginCell().MustStoreUInt(1, 1).MustStoreRef(target).EndCell()
	prunedLeaf := BeginCell().MustStoreUInt(0xdef, 12).EndCell()
	prunedBranch := BeginCell().MustStoreUInt(0, 1).MustStoreRef(prunedLeaf).EndCell()
	root := BeginCell().MustStoreRef(keptBranch).MustStoreRef(prunedBranch).EndCell()

	// The target may have been observed through an equal synthetic dictionary
	// view. Selection therefore carries hashes, not a trace rooted at root.
	loaded := map[Hash]struct{}{
		root.HashKey():       {},
		keptBranch.HashKey(): {},
		target.HashKey():     {},
	}
	proof, err := root.CreateHashUsageProof(func(hash Hash) bool {
		_, ok := loaded[hash]
		return ok
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatal(err)
	}
	kept := body.MustPeekRef(0)
	if kept.IsSpecial() || kept.MustPeekRef(0).IsSpecial() {
		t.Fatal("hash-selected path was pruned")
	}
	if got := kept.MustPeekRef(0).MustBeginParse().MustLoadUInt(12); got != 0xabc {
		t.Fatalf("selected payload = %x, want abc", got)
	}
	if other := body.MustPeekRef(1); !other.IsSpecial() || other.GetType() != PrunedCellType {
		t.Fatalf("unselected branch type = %v special=%t, want pruned", other.GetType(), other.IsSpecial())
	}
}

func TestCreateHashUsageProofRejectsNilSelector(t *testing.T) {
	root := BeginCell().EndCell()
	if _, err := root.CreateHashUsageProof(nil); err == nil {
		t.Fatal("nil selector was accepted")
	}
}

func TestCreateHashUsageProofAlwaysMaterializesRoot(t *testing.T) {
	left := BeginCell().MustStoreRef(BeginCell().MustStoreUInt(1, 1).EndCell()).EndCell()
	right := BeginCell().MustStoreRef(BeginCell().MustStoreUInt(0, 1).EndCell()).EndCell()
	root := BeginCell().MustStoreRef(left).MustStoreRef(right).EndCell()

	proof, err := root.CreateHashUsageProof(func(Hash) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	body := proof.MustPeekRef(0)
	if body.IsSpecial() {
		t.Fatalf("proof body type = %v, want ordinary root", body.GetType())
	}
	if body.RefsNum() != 2 {
		t.Fatalf("proof body refs = %d, want 2", body.RefsNum())
	}
	for i := 0; i < 2; i++ {
		ref := body.MustPeekRef(i)
		if !ref.IsSpecial() || ref.GetType() != PrunedCellType {
			t.Fatalf("proof body ref %d type = %v special=%t, want pruned", i, ref.GetType(), ref.IsSpecial())
		}
	}
}
