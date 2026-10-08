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

// TestCreateHashUsageProofPrunesUnreadLeaves pins the collated-data rule: a
// leaf the selector declines becomes a pruned branch whether or not it is
// resident. The generic builders keep CellBuilder::create_pruned_branch's
// loaded-leaf exception (TestReadSetKeepsUnloadedOrdinaryLeafRef pins it against
// the reference over an in-memory tree); here the selector has already said the
// cell is one the reference collator never loaded, and the reference prunes
// those. Before this rule every unread message body next to a read envelope
// travelled whole in the collated data — 188 kB of a 1.59 MB post-split proof
// on the collator's deep-queue fixture.
func TestCreateHashUsageProofPrunesUnreadLeaves(t *testing.T) {
	const branches = 4
	loaded := map[Hash]struct{}{}
	bodies := make([]*Cell, 0, branches)
	root := BeginCell()
	for i := 0; i < branches; i++ {
		// A body wider than a pruned branch, so keeping it whole would be the
		// larger proof, and a fork of read cells above it.
		body := BeginCell().MustStoreUInt(0x0f8a7ea5, 32).MustStoreUInt(uint64(i), 64).
			MustStoreSlice(bytes.Repeat([]byte{byte(0xa0 + i)}, 64), 512).EndCell()
		message := BeginCell().MustStoreUInt(uint64(i), 8).MustStoreRef(body).EndCell()
		envelope := BeginCell().MustStoreUInt(4, 4).MustStoreRef(message).EndCell()
		loaded[message.HashKey()] = struct{}{}
		loaded[envelope.HashKey()] = struct{}{}
		bodies = append(bodies, body)
		root.MustStoreRef(envelope)
	}
	source := root.EndCell()
	loaded[source.HashKey()] = struct{}{}
	isLoaded := func(hash Hash) bool {
		_, ok := loaded[hash]
		return ok
	}

	check := func(t *testing.T, proof *Cell) {
		t.Helper()
		body, err := UnwrapProof(proof, source.Hash())
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < branches; i++ {
			message := body.MustPeekRef(i).MustPeekRef(0)
			if message.IsSpecial() {
				t.Fatalf("branch %d: read message root was pruned", i)
			}
			leaf := message.MustPeekRef(0)
			if !leaf.IsSpecial() || leaf.GetType() != PrunedCellType {
				t.Fatalf("branch %d: unread leaf type = %v special=%t, want pruned branch", i, leaf.GetType(), leaf.IsSpecial())
			}
			if !bytes.Equal(leaf.Hash(0), bodies[i].Hash()) {
				t.Fatalf("branch %d: pruned leaf does not carry the body hash", i)
			}
		}
	}

	serial, err := source.CreateHashUsageProof(isLoaded)
	if err != nil {
		t.Fatal(err)
	}
	check(t, serial)

	// The same selection through the branch-worker walk, over the threshold
	// that arms it; the two builds must agree byte for byte.
	parallel, err := source.CreateHashUsageProofResolvedSizedParallel(isLoaded, nil, proofParallelMinCells, 4)
	if err != nil {
		t.Fatal(err)
	}
	check(t, parallel)
	if !bytes.Equal(serial.ToBOC(), parallel.ToBOC()) {
		t.Fatal("serial and parallel hash usage proofs differ")
	}

	// The read-set proof keeps the loaded-leaf rule: the same tree recorded
	// through a ReadSet leaves the unread body whole, as the reference does over
	// a tree whose every cell is loaded.
	rs := NewReadSet(source)
	rootLoader := rs.Root().MustBeginParse()
	for i := 0; i < branches; i++ {
		envelope, err := rootLoader.LoadRefCell()
		if err != nil {
			t.Fatal(err)
		}
		message, err := envelope.MustBeginParse().LoadRefCell()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = message.BeginParse(); err != nil {
			t.Fatal(err)
		}
	}
	readSetProof, err := rs.Proof()
	if err != nil {
		t.Fatal(err)
	}
	readSetBody, err := UnwrapProof(readSetProof, source.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if leaf := readSetBody.MustPeekRef(0).MustPeekRef(0).MustPeekRef(0); leaf.IsSpecial() {
		t.Fatalf("read-set proof pruned a resident leaf, type %v", leaf.GetType())
	}
}
