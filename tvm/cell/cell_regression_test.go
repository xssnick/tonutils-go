package cell

import "testing"

func TestCellToBuilderClearsParsedTopUpBit(t *testing.T) {
	src := BeginCell().MustStoreUInt(0b101, 3).EndCell()
	parsed, err := FromBOC(src.ToBOCWithOptions(BOCSerializeOptions{}))
	if err != nil {
		t.Fatal(err)
	}

	builder := parsed.ToBuilder()
	builder.MustStoreUInt(0, 1)
	got := builder.EndCell().MustBeginParse().MustLoadUInt(4)
	if got != 0b1010 {
		t.Fatalf("unexpected appended bits: got %04b want 1010", got)
	}
}

func TestSliceToCellRecomputesOrdinaryLevelMask(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	branch := BeginCell().MustStoreRef(leaf).EndCell()
	pruned, err := createPrunedBranchFromCell(branch, 1)
	if err != nil {
		t.Fatal(err)
	}
	root := BeginCell().MustStoreUInt(1, 1).MustStoreRef(pruned).EndCell()
	if root.Level() == 0 {
		t.Fatal("test root should have non-zero level")
	}

	bitsOnly, err := root.MustBeginParse().PreloadSubslice(root.BitsSize(), 0)
	if err != nil {
		t.Fatal(err)
	}
	cell, err := bitsOnly.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	rebuilt := bitsOnly.ToBuilder().EndCell()
	if cell.Level() != 0 {
		t.Fatalf("ordinary slice cell kept stale level: got %d want 0", cell.Level())
	}
	if cell.HashKey() != rebuilt.HashKey() {
		t.Fatal("slice ToCell should match rebuilding through ToBuilder")
	}
}

func TestSliceToBuilderCopiesUnalignedBits(t *testing.T) {
	src := BeginCell().MustStoreUInt(0b101101001, 9).EndCell()
	slice := src.MustBeginParse()
	if err := slice.SkipBits(3); err != nil {
		t.Fatal(err)
	}

	rebuilt := slice.ToBuilder().EndCell()
	got := rebuilt.MustBeginParse().MustLoadUInt(6)
	if got != 0b101001 {
		t.Fatalf("rebuilt bits = %06b, want 101001", got)
	}
}

func TestVirtualizedLazyRefLoadsByRawHashAndChecksVisibleHash(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xCD, 8).EndCell()
	branch := BeginCell().MustStoreRef(leaf).EndCell()
	pruned, err := createPrunedBranchFromCell(branch, 1)
	if err != nil {
		t.Fatal(err)
	}
	source := BeginCell().MustStoreRef(pruned).EndCell()
	if source.HashKey() == source.HashKey(0) {
		t.Fatal("test source should have different top and level-0 hashes")
	}

	loader := &testLazyLoader{cells: map[Hash]*Cell{source.HashKey(): source}}
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(source), loader.LoadCell)
	virtual := lazy.Virtualize(0)

	loaded, err := virtual.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	if loader.lastHash != source.HashKey() {
		t.Fatalf("loader was called with visible hash: got %x want %x", loader.lastHash, source.Hash())
	}
	if loaded.BaseCell().HashKey() != source.HashKey(0) {
		t.Fatal("virtualized lazy ref should expose the effective-level hash")
	}
}

func TestLazyBOCParseCopiesPayloadUnlessNoCopy(t *testing.T) {
	root := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{WithTopHash: true})

	roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(boc), BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cellOffset := firstBOCCellOffset(t, boc)
	bodyOffset := cellOffset + 2 + hashSize + depthSize
	boc[bodyOffset] = 0xCD

	got := roots[0].MustBeginParse().MustLoadUInt(8)
	if got != 0xAB {
		t.Fatalf("lazy parse aliased source boc payload: got %x want ab", got)
	}
}

func TestUsageProofReusesLoadedLazyCellsForDuplicateHash(t *testing.T) {
	shared := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	root := BeginCell().MustStoreRef(shared).MustStoreRef(shared).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{shared.HashKey(): shared}}
	lazyRoot := cellWithLazyRefsFromCell(root, loader.LoadCell)

	builder := NewMerkleProofBuilder(lazyRoot)
	left, err := builder.Root().MustBeginParse().LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if left.MustLoadUInt(8) != 0xAA {
		t.Fatal("unexpected shared value")
	}

	callsBeforeProof := loader.calls
	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	if proof == nil {
		t.Fatal("proof is nil")
	}
	if got := loader.calls - callsBeforeProof; got != 0 {
		t.Fatalf("proof build reloaded shared lazy refs: got %d extra loads, want 0", got)
	}
}

func TestUsageProofReusesLoadedLazyRoot(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	root := BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(leaf).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{root.HashKey(): root}}
	lazyRoot := mustCreateLazyPrunedRef(t, lazyRefFromCell(root), loader.LoadCell)

	builder := NewMerkleProofBuilder(lazyRoot)
	rootSlice, err := builder.Root().BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	if rootSlice.MustLoadUInt(8) != 0xAB {
		t.Fatal("unexpected root value")
	}

	callsBeforeProof := loader.calls
	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	if proof == nil {
		t.Fatal("proof is nil")
	}
	if got := loader.calls - callsBeforeProof; got != 0 {
		t.Fatalf("proof build reloaded lazy root: got %d extra loads, want 0", got)
	}
}
