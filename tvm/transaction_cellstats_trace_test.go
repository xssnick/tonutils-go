package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionCellStatsTracesReusedPredecessorSubtree(t *testing.T) {
	deep := cell.BeginCell().MustStoreUInt(0x3a, 8).EndCell()
	reused := cell.BeginCell().MustStoreRef(
		cell.BeginCell().MustStoreRef(deep).EndCell(),
	).EndCell()
	discarded := cell.BeginCell().MustStoreRef(
		cell.BeginCell().MustStoreUInt(0xac, 8).EndCell(),
	).EndCell()
	predecessor := cell.BeginCell().MustStoreRef(reused).MustStoreRef(discarded).EndCell()

	read := cell.NewReadSet(predecessor)
	predecessorSlice := read.Root().MustBeginParse()
	tracedReused, err := predecessorSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}

	// This is the shape which triggered the live C++ rejection: the VM builds
	// a new data cell but stores the old code root under it without reading the
	// code DAG. State-size accounting must trace that reused DAG itself.
	nextData := cell.BeginCell().
		MustStoreRef(tracedReused).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0x4e, 8).EndCell()).
		EndCell()
	if _, err = transactionCellStatsForRoots(true, nextData, tracedReused); err != nil {
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
	provenSlice := proven.MustBeginParse()
	provenReused, err := provenSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	levelOne, err := provenReused.MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatalf("load reused code subtree: %v", err)
	}
	if _, err = levelOne.MustBeginParse().LoadRefCell(); err != nil {
		t.Fatalf("load deep reused code cell: %v", err)
	}

	provenDiscarded, err := provenSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if provenDiscarded.GetType() != cell.PrunedCellType {
		t.Fatal("predecessor-only subtree is materialized")
	}
}

func TestTransactionCellStatsLeavesProofTraceUntouchedWhenDisabled(t *testing.T) {
	reused := cell.BeginCell().MustStoreRef(
		cell.BeginCell().MustStoreUInt(0x3a, 8).EndCell(),
	).EndCell()
	predecessor := cell.BeginCell().MustStoreRef(reused).EndCell()

	read := cell.NewReadSet(predecessor)
	predecessorSlice := read.Root().MustBeginParse()
	tracedReused, err := predecessorSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = transactionCellStatsForRoots(false, tracedReused); err != nil {
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
	provenReused, err := proven.MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if provenReused.GetType() != cell.PrunedCellType {
		t.Fatal("disabled state-size tracing materialized predecessor state")
	}
}
