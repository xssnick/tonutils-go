package cell

import (
	"bytes"
	"testing"
)

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

// The resolved variant exists to stop a proof build from paging a disk-backed
// predecessor cell in a second time. What it must never do is change the proof:
// the recorder's instance and the one the loader would return are the same
// cell, so the two builds are byte-identical and only the read count moves.
func TestCreateHashUsageProofResolvedMatchesUnresolvedOverALazyTree(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	branch := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(leaf).EndCell()
	untouched := BeginCell().MustStoreUInt(0x33, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0x44, 8).EndCell()).
		EndCell()
	resident := BeginCell().MustStoreUInt(0xdead, 16).
		MustStoreRef(branch).
		MustStoreRef(untouched).
		EndCell()

	build := func(resolve bool) (*Cell, int) {
		loader := &countingLazyLoader{cells: map[Hash]*Cell{
			branch.HashKey():    branch,
			untouched.HashKey(): untouched,
		}}
		dsc1, dsc2 := resident.descriptors(resident.getLevelMask())
		lazyRoot := mustCreateWithLazyRefsUnsafe(t,
			uint16(dsc1)<<8|uint16(dsc2),
			serializedCellData(resident),
			significantHashes(resident),
			significantDepths(resident),
			[]LazyRef{lazyRefFromCell(branch), lazyRefFromCell(untouched)},
			loader.LoadCell,
		)

		rs := NewReadSet(lazyRoot)
		rootSlice, err := rs.Root().BeginParse()
		if err != nil {
			t.Fatalf("parse root: %v", err)
		}
		if _, err = rootSlice.LoadUInt(16); err != nil {
			t.Fatalf("load root tag: %v", err)
		}
		branchRef, err := rootSlice.LoadRefCell()
		if err != nil {
			t.Fatalf("take the branch ref: %v", err)
		}
		branchSlice, err := branchRef.BeginParse()
		if err != nil {
			t.Fatalf("parse branch: %v", err)
		}
		if _, err = branchSlice.LoadUInt(8); err != nil {
			t.Fatalf("load branch tag: %v", err)
		}
		if _, err = branchSlice.LoadRefCell(); err != nil {
			t.Fatalf("take the leaf ref: %v", err)
		}

		selected := func(hash Hash) bool {
			_, read := rs.Contains(hash)
			return read
		}
		var resolver func(Hash) *Cell
		if resolve {
			resolver = rs.RecordedCell
		}
		proof, err := lazyRoot.WithoutTrace().CreateHashUsageProofResolved(selected, resolver)
		if err != nil {
			t.Fatalf("build hash usage proof: %v", err)
		}
		return proof, loader.calls[branch.HashKey()]
	}

	unresolved, unresolvedLoads := build(false)
	resolved, resolvedLoads := build(true)

	if !bytes.Equal(unresolved.ToBOC(), resolved.ToBOC()) {
		t.Fatal("handing the recorder's cells back changed the proof")
	}
	if unresolved.HashKey() != resolved.HashKey() {
		t.Fatal("resolved and unresolved proofs have different roots")
	}
	if resolvedLoads >= unresolvedLoads {
		t.Fatalf("the branch was loaded %d times resolved and %d times unresolved",
			resolvedLoads, unresolvedLoads)
	}
}
