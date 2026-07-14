package tlb

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// TestBlkPrevInfoPrunedIsExplicit verifies that parsing a block header whose
// prev-block references were pruned away in a Merkle proof marks the result
// explicitly instead of silently returning zeroed references, and that such a
// header refuses to serialize the missing data back.
//
// C++ reference behaviour: proofs may legally prune prev cells (proof checks
// like check_block_header never read them), while paths that DO need them fail
// explicitly ("cannot unpack previous block reference from block header",
// block::unpack_block_prev_blk_ext, ton/crypto/block/block.cpp:1912-1925).
func TestBlkPrevInfoPrunedIsExplicit(t *testing.T) {
	root, err := cell.FromBOC(mainnetBlockBOC)
	if err != nil {
		t.Fatal(err)
	}

	// full block: prev refs must parse normally and not be marked pruned
	var full Block
	if err = LoadFromCell(&full, root.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if full.BlockInfo.PrevRef.Pruned {
		t.Fatal("full block prev ref must not be marked pruned")
	}
	if full.BlockInfo.PrevRef.Prev1.SeqNo != full.BlockInfo.SeqNo-1 {
		t.Fatalf("unexpected prev seqno %d for block %d",
			full.BlockInfo.PrevRef.Prev1.SeqNo, full.BlockInfo.SeqNo)
	}

	// Build a header cell whose prev-block reference is a pruned branch, the
	// way C++-produced Merkle proofs (e.g. lite-server block proofs) deliver it
	// when the prev cell was not visited. Note that the Go proof builder
	// (Cell.CreateProof) keeps leaf cells as-is instead of pruning them, so the
	// pruned branch is constructed explicitly here.
	infoCell, err := root.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	masterRef, err := infoCell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	prunedPrev, err := cell.CreatePrunedBranch(root, 1, 0) // any pruned branch stand-in
	if err != nil {
		t.Fatalf("failed to create pruned branch: %v", err)
	}
	if !prunedPrev.IsSpecial() {
		t.Fatal("stand-in prev cell must be a special pruned branch")
	}
	prunedInfo, err := infoCell.RebuildWithRefs([]*cell.Cell{masterRef, prunedPrev})
	if err != nil {
		t.Fatalf("failed to rebuild header cell: %v", err)
	}

	var proofHeader BlockHeader
	if err = LoadFromCell(&proofHeader, prunedInfo.MustBeginParse()); err != nil {
		t.Fatalf("failed to parse header with pruned prev: %v", err)
	}

	if !proofHeader.PrevRef.Pruned {
		t.Fatal("pruned prev ref must be marked as pruned")
	}
	if proofHeader.PrevRef.Prev1.SeqNo != 0 || proofHeader.PrevRef.Prev2 != nil {
		t.Fatal("pruned prev ref must not carry any data")
	}
	if proofHeader.MasterRef == nil {
		t.Fatal("master ref was kept and must parse")
	}

	// serializing a header with pruned prev refs must fail loudly
	if _, err = proofHeader.ToCell(); err == nil {
		t.Fatal("expected serialization of pruned prev info to fail")
	} else if !strings.Contains(err.Error(), "pruned") {
		t.Fatalf("expected a pruned-specific error, got: %v", err)
	}

	// the non-pruned header must still serialize and round-trip
	headerCell, err := full.BlockInfo.ToCell()
	if err != nil {
		t.Fatalf("failed to serialize full header: %v", err)
	}
	var reparsed BlockHeader
	if err = LoadFromCell(&reparsed, headerCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to reparse full header: %v", err)
	}
	if reparsed.PrevRef.Pruned ||
		reparsed.PrevRef.Prev1.SeqNo != full.BlockInfo.PrevRef.Prev1.SeqNo ||
		!strings.EqualFold(string(reparsed.PrevRef.Prev1.RootHash), string(full.BlockInfo.PrevRef.Prev1.RootHash)) {
		t.Fatal("full header prev ref round-trip mismatch")
	}
}
