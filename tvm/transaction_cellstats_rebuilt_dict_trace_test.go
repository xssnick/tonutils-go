package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTransactionCellStatsTracesReusedSubtreeThroughRebuiltDictionary(t *testing.T) {
	leftLeaf := cell.BeginCell().MustStoreUInt(0x11, 8).EndCell()
	leftValue := cell.BeginCell().MustStoreRef(leftLeaf).EndCell()
	rightValue := cell.BeginCell().MustStoreRef(
		cell.BeginCell().MustStoreUInt(0x22, 8).EndCell(),
	).EndCell()
	oldDict := cell.NewDict(8)
	if err := oldDict.Set(cell.BeginCell().MustStoreUInt(0x20, 8).EndCell(), leftValue); err != nil {
		t.Fatal(err)
	}
	if err := oldDict.Set(cell.BeginCell().MustStoreUInt(0xe0, 8).EndCell(), rightValue); err != nil {
		t.Fatal(err)
	}
	oldData := cell.BeginCell().MustStoreRef(oldDict.AsCell()).EndCell()
	predecessor := cell.BeginCell().MustStoreRef(oldData).EndCell()

	read := cell.NewReadSet(predecessor)
	tracedData, err := read.Root().MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	tracedDictRoot, err := tracedData.MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	nextDict := tracedDictRoot.AsDict(8)
	if err = nextDict.Set(
		cell.BeginCell().MustStoreUInt(0xf0, 8).EndCell(),
		cell.BeginCell().MustStoreUInt(0x33, 8).EndCell(),
	); err != nil {
		t.Fatal(err)
	}
	nextData := cell.BeginCell().MustStoreRef(nextDict.AsCell()).EndCell()

	if _, err = transactionCellStatsForRoots(true, nextData); err != nil {
		t.Fatal(err)
	}

	proof, err := read.Proof()
	if err != nil {
		t.Fatal(err)
	}
	proven, err := cell.UnwrapProof(proof, predecessor.Hash())
	if err != nil {
		t.Fatal(err)
	}
	provenData, err := proven.MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	provenDictRoot, err := provenData.MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	value, err := provenDictRoot.AsDict(8).LoadValue(
		cell.BeginCell().MustStoreUInt(0x20, 8).EndCell(),
	)
	if err != nil {
		t.Fatalf("load reused dictionary value: %v", err)
	}
	reused, err := value.LoadRefCell()
	if err != nil {
		t.Fatalf("load reused dictionary subtree: %v", err)
	}
	if reused.HashKey() != leftLeaf.HashKey() {
		t.Fatalf("reused subtree hash %x, want %x", reused.Hash(), leftLeaf.Hash())
	}
}

func TestTransactionCellStatsRejectsPrunedBranch(t *testing.T) {
	root := cell.BeginCell().
		MustStoreRef(cell.BeginCell().MustStoreUInt(0x11, 8).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreRef(
			cell.BeginCell().MustStoreUInt(0x55, 8).EndCell(),
		).EndCell()).
		EndCell()
	read := cell.NewReadSet(root)
	rootSlice, err := read.Root().BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	used, err := rootSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = transactionCellStatsForRoots(true, used); err != nil {
		t.Fatal(err)
	}
	proof, err := read.Proof()
	if err != nil {
		t.Fatal(err)
	}
	proven, err := cell.UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatal(err)
	}
	provenSlice := proven.MustBeginParse()
	if _, err = provenSlice.LoadRefCell(); err != nil {
		t.Fatal(err)
	}
	pruned, err := provenSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if pruned.GetType() != cell.PrunedCellType {
		t.Fatal("fixture did not produce a pruned branch")
	}

	_, err = transactionCellStatsForRoots(false, pruned)
	if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeVirtualization {
		t.Fatalf("error = %v, want virtualization", err)
	}
}
