package cell

import "testing"

// lazyRefOf describes c the way a celldb-backed parent holds it: hashes and
// depths only, no body.
func lazyRefOf(c *Cell) LazyRef {
	mask := c.getLevelMask()
	n := mask.getHashesCount()
	hashes := make([]byte, n*hashSize)
	depths := make([]uint16, n)
	for i := 0; i < n; i++ {
		copy(hashes[i*hashSize:], c.getHash(i))
		depths[i] = c.getDepth(i)
	}
	return LazyRef{LevelMask: mask, Hashes: hashes, Depths: depths}
}

// pagedTwin rebuilds c with the same body, hashes and depths, but with every
// reference replaced by an unresolved lazy placeholder. It is the shape a state
// read out of a real cell database has: the cell itself is in memory, the
// subtrees below it are not, and reaching one costs a disk read. The returned
// counter is incremented once per resolution.
func pagedTwin(t *testing.T, c *Cell) (*Cell, *int) {
	t.Helper()

	bodies := map[Hash]*Cell{}
	refs := make([]LazyRef, c.refsCount())
	for i := 0; i < c.refsCount(); i++ {
		ref, err := c.PeekRef(i)
		if err != nil {
			t.Fatalf("peek ref %d: %v", i, err)
		}
		bodies[ref.HashKey()] = ref
		refs[i] = lazyRefOf(ref)
	}

	loads := 0
	loader := func(h Hash) (*Cell, error) {
		loads++
		return bodies[h], nil
	}

	mask := c.getLevelMask()
	d1, d2 := c.descriptors(mask)
	n := mask.getHashesCount()
	hashes := make([]byte, n*hashSize)
	depths := make([]uint16, n)
	for i := 0; i < n; i++ {
		copy(hashes[i*hashSize:], c.getHash(i))
		depths[i] = c.getDepth(i)
	}
	body := make([]byte, c.SerializedBOCBodySize())
	c.SerializeBOCBodyTo(body)

	twin, err := CreateWithLazyRefsUnsafe(uint16(d1)<<8|uint16(d2), body, hashes, depths, refs, loader)
	if err != nil {
		t.Fatalf("build paged twin: %v", err)
	}
	if twin.HashKey() != c.HashKey() {
		t.Fatalf("paged twin must keep the hash: %x vs %x", twin.HashKey(), c.HashKey())
	}
	return twin, &loads
}

// TestReadSetPrunableFrontierStaysCurrentWhileReading pins that a question asked
// early does not fix the answer for every later one. The proof-size estimator
// asks per transaction, interleaved with the reads of the next, and a frontier
// frozen at the first question reports every boundary read after it as unknown —
// which sends the estimator's walk down subtrees the block will carry as pruned
// branches.
func TestReadSetPrunableFrontierStaysCurrentWhileReading(t *testing.T) {
	leafA := BeginCell().MustStoreUInt(0xA1, 16).EndCell()
	leafB := BeginCell().MustStoreUInt(0xB1, 16).EndCell()
	branchA := BeginCell().MustStoreUInt(0xAA, 16).MustStoreRef(leafA).EndCell()
	branchB := BeginCell().MustStoreUInt(0xBB, 16).MustStoreRef(leafB).EndCell()
	root := BeginCell().MustStoreUInt(0, 8).MustStoreRef(branchA).MustStoreRef(branchB).EndCell()

	rs := NewReadSet(root)
	rootSlice, err := rs.Root().BeginParse()
	if err != nil {
		t.Fatal(err)
	}

	// The first question. In the unfixed code this is what freezes the frontier.
	if _, known := rs.Prunable(branchA.HashKey()); !known {
		t.Fatalf("branchA is an unread child of the read root and must be prunable")
	}

	branchASlice, err := rootSlice.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if _, known := rs.Prunable(leafA.HashKey()); !known {
		t.Fatalf("leafA became an unread child of a read cell and must be prunable")
	}

	if _, err = branchASlice.LoadRef(); err != nil {
		t.Fatal(err)
	}
	if _, read := rs.Contains(leafA.HashKey()); !read {
		t.Fatalf("leafA was parsed and must be recorded")
	}
	if _, known := rs.Prunable(leafA.HashKey()); !known {
		t.Fatalf("a read cell stays prunable")
	}

	if _, err = rootSlice.LoadRef(); err != nil {
		t.Fatal(err)
	}
	if _, known := rs.Prunable(leafB.HashKey()); !known {
		t.Fatalf("leafB became an unread child of a read cell and must be prunable")
	}
}

// TestReadSetPrunableIsMaterializationInvariant pins that membership does not
// depend on whether the predecessor happens to be in memory. A lazy placeholder
// carries the hash of the subtree it stands for, so it is the same boundary the
// resident cell is; answering otherwise would make every size estimate derived
// from the frontier differ between a node with a warm cache and one without.
func TestReadSetPrunableIsMaterializationInvariant(t *testing.T) {
	child := BeginCell().MustStoreUInt(0xC0FFEE, 32).EndCell()
	sibling := BeginCell().MustStoreUInt(0xBEEF, 32).EndCell()
	root := BeginCell().MustStoreUInt(1, 8).MustStoreRef(child).MustStoreRef(sibling).EndCell()

	paged, loads := pagedTwin(t, root)

	answer := func(source *Cell) (bool, bool) {
		rs := NewReadSet(source)
		if _, err := rs.Root().BeginParse(); err != nil {
			t.Fatal(err)
		}
		_, a := rs.Prunable(child.HashKey())
		_, b := rs.Prunable(sibling.HashKey())
		return a, b
	}

	residentChild, residentSibling := answer(root)
	pagedChild, pagedSibling := answer(paged)

	if !residentChild || !residentSibling {
		t.Fatalf("resident: both children of the read root must be prunable, got %v/%v", residentChild, residentSibling)
	}
	if pagedChild != residentChild || pagedSibling != residentSibling {
		t.Fatalf("paged state answers differently from a resident one: %v/%v vs %v/%v",
			pagedChild, pagedSibling, residentChild, residentSibling)
	}
	if *loads != 0 {
		t.Fatalf("answering membership must not page anything in, got %d resolutions", *loads)
	}
}

// TestCellStorageStatProofIsResidencyInvariant is the property the block-size
// estimator depends on: the proof half of the storage stat must count the same
// bytes for a collator holding the predecessor state in RAM and for one reading
// it out of a cell database, and must not turn the estimate into disk reads.
//
// Two proofs are added with a read in between, because that is how the collator
// uses it — one AddProof per transaction, over a destination that reuses most of
// the predecessor unchanged.
func TestCellStorageStatProofIsResidencyInvariant(t *testing.T) {
	keptDeep := BeginCell().MustStoreUInt(0xDEE9, 16).EndCell()
	kept := BeginCell().MustStoreUInt(0x1111, 32).MustStoreRef(keptDeep).EndCell()
	later := BeginCell().MustStoreUInt(0x2222, 32).MustStoreRef(
		BeginCell().MustStoreUInt(0x3333, 32).EndCell()).EndCell()
	root := BeginCell().MustStoreUInt(7, 8).MustStoreRef(kept).MustStoreRef(later).EndCell()

	run := func(source *Cell) StorageStat {
		rs := NewReadSet(source)
		rootSlice, err := rs.Root().BeginParse()
		if err != nil {
			t.Fatal(err)
		}

		// The destination the collator built: a fresh cell that carries the
		// predecessor's first subtree unchanged, by reference.
		keptRef, err := rootSlice.MustToCell().PeekRef(0)
		if err != nil {
			t.Fatal(err)
		}
		destination := BeginCell().MustStoreUInt(0xFF, 8).MustStoreRef(keptRef).EndCell()

		stat := NewCellStorageStat()
		if err = stat.AddProof(destination, rs); err != nil {
			t.Fatal(err)
		}

		// More of the predecessor is read, then the next transaction's proof is
		// added — the interleaving the estimator actually performs.
		if _, err = rootSlice.LoadRef(); err != nil {
			t.Fatal(err)
		}
		second := BeginCell().MustStoreUInt(0xEE, 8).MustStoreRef(keptRef).EndCell()
		if err = stat.AddProof(second, rs); err != nil {
			t.Fatal(err)
		}
		return stat.TotalStat()
	}

	paged, loads := pagedTwin(t, root)
	residentStat := run(root)
	pagedStat := run(paged)

	if pagedStat != residentStat {
		t.Fatalf("proof stat depends on residency: paged %+v vs resident %+v", pagedStat, residentStat)
	}
	// The one read above resolves the second subtree; the estimator must add
	// nothing on top of it.
	if *loads != 1 {
		t.Fatalf("the estimator paged in %d subtrees beyond the one that was read", *loads-1)
	}
}
