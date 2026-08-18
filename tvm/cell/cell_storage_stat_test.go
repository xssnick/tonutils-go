package cell

import "testing"

func TestCellStorageStatCountsCellsProofsAndSharedRefs(t *testing.T) {
	external := BeginCell().MustStoreUInt(0xa, 4).EndCell()
	shared := BeginCell().MustStoreUInt(0xcc, 8).EndCell()
	root := BeginCell().
		MustStoreUInt(0xdd, 8).
		MustStoreRef(external).
		MustStoreRef(shared).
		MustStoreRef(shared).
		EndCell()

	// A recorded cell is the boundary of the proof: everything below it is
	// already accounted for by whoever read it.
	read := NewReadSet(external)
	read.Record(external)

	stat := NewCellStorageStat()
	if err := stat.AddCell(root); err != nil {
		t.Fatal(err)
	}
	if err := stat.AddProof(root, read); err != nil {
		t.Fatal(err)
	}

	wantCells := StorageStat{Cells: 3, Bits: 20, InternalRefs: 4}
	if got := stat.stat; got != wantCells {
		t.Fatalf("cell stat = %+v, want %+v", got, wantCells)
	}
	wantProof := StorageStat{Cells: 2, Bits: 16, InternalRefs: 3, ExternalRefs: 1}
	if got := stat.proofStat; got != wantProof {
		t.Fatalf("proof stat = %+v, want %+v", got, wantProof)
	}
	wantTotal := StorageStat{Cells: 5, Bits: 36, InternalRefs: 7, ExternalRefs: 1}
	if got := stat.TotalStat(); got != wantTotal {
		t.Fatalf("total stat = %+v, want %+v", got, wantTotal)
	}
}

func TestCellStorageStatIgnoresNilRoots(t *testing.T) {
	stat := NewCellStorageStat()
	if err := stat.AddCell(nil); err != nil {
		t.Fatal(err)
	}
	if err := stat.AddProof(nil, NewReadSet(nil)); err != nil {
		t.Fatal(err)
	}
	if got := stat.TotalStat(); got != (StorageStat{}) {
		t.Fatalf("nil roots produced stat %+v", got)
	}
}

func TestCellStorageStatLoadsLazyReferences(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xaa, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{leaf.HashKey(): leaf}}
	lazyRoot := cellWithLazyRefsFromCell(root, loader.LoadCell)

	stat := NewCellStorageStat()
	if err := stat.AddCell(lazyRoot); err != nil {
		t.Fatal(err)
	}
	want := StorageStat{Cells: 2, Bits: 8, InternalRefs: 2}
	if got := stat.TotalStat(); got != want {
		t.Fatalf("lazy cell stat = %+v, want %+v", got, want)
	}
	if loader.calls != 1 {
		t.Fatalf("lazy loader calls = %d, want 1", loader.calls)
	}
}

// A cell this proof has already carried in full stays an ordinary reference on
// every later edge, even after the read set has learned its hash.
//
// This is the estimator's only defence against a class of cell the recorder
// cannot tell apart from a predecessor subtree: one the transition rebuilt and
// then read back. Such a cell is recorded — "parsed through the recording
// trace" — but the source tree never held it, so the produced update never
// prunes onto it (see readset_source_graph.go, which exists to separate exactly
// those two questions, and readset_update.go, which prunes by the source graph
// and not by the record). Charging it as a 40-byte boundary is charging for
// bytes the block will never contain.
//
// It is not hypothetical arithmetic. On the mainnet-heavy collation fixture 113
// such hashes, over 138 edges, inflated the admission estimate by 4,732 B
// against a 1,048,576 B budget and stopped the block six inbound messages
// earlier than the reference implementation, whose predicate is per-object
// usage-tree provenance and therefore cannot drift at all.
func TestCellStorageStatKeepsAlreadySerializedCellsInternal(t *testing.T) {
	// Stands for a cell the transition rebuilt: nothing knows it when the first
	// proof is charged.
	rebuilt := BeginCell().MustStoreUInt(0xbe, 8).EndCell()
	first := BeginCell().MustStoreUInt(0x01, 8).MustStoreRef(rebuilt).EndCell()
	second := BeginCell().MustStoreUInt(0x02, 8).MustStoreRef(rebuilt).EndCell()

	read := NewReadSet(rebuilt)
	stat := NewCellStorageStat()
	if err := stat.AddProof(first, read); err != nil {
		t.Fatal(err)
	}
	want := StorageStat{Cells: 2, Bits: 16, InternalRefs: 2}
	if got := stat.proofStat; got != want {
		t.Fatalf("first proof stat = %+v, want %+v", got, want)
	}

	// The collation reads the rebuilt cell back, which is what puts its hash in
	// the record halfway through the block.
	read.Record(rebuilt)

	if err := stat.AddProof(second, read); err != nil {
		t.Fatal(err)
	}
	// second is new and carries a body; rebuilt is a repeat edge onto a body this
	// proof already holds, so it costs an internal reference and nothing else.
	want = StorageStat{Cells: 3, Bits: 24, InternalRefs: 4}
	if got := stat.proofStat; got != want {
		t.Fatalf("second proof stat = %+v, want %+v — a serialized cell became a boundary", got, want)
	}
}

// The other half of the same rule: a cell the record already knows on first
// sight is a boundary, and stays one on every edge. It never enters the memo, so
// the ordering above cannot demote a real pruned branch to a 3-byte reference.
//
// This is a NEGATIVE CONTROL, not a gate: it passes both with and without the
// serialized-before-prunable ordering, because the boundary here is prunable
// from the first sight and is therefore never serialized. What it fails on is
// the ordering being applied too widely — demote this boundary and the counts
// become InternalRefs 3 / ExternalRefs 1. The gate for the ordering itself is
// TestCellStorageStatKeepsAlreadySerializedCellsInternal above, which is the
// only one of the pair that fails when the fix is reverted.
func TestCellStorageStatKeepsKnownCellsExternalOnEveryEdge(t *testing.T) {
	boundary := BeginCell().MustStoreUInt(0xbd, 8).EndCell()
	first := BeginCell().MustStoreUInt(0x01, 8).MustStoreRef(boundary).EndCell()
	second := BeginCell().MustStoreUInt(0x02, 8).MustStoreRef(boundary).EndCell()

	read := NewReadSet(boundary)
	read.Record(boundary)

	stat := NewCellStorageStat()
	if err := stat.AddProof(first, read); err != nil {
		t.Fatal(err)
	}
	if err := stat.AddProof(second, read); err != nil {
		t.Fatal(err)
	}
	want := StorageStat{Cells: 2, Bits: 16, InternalRefs: 2, ExternalRefs: 2}
	if got := stat.proofStat; got != want {
		t.Fatalf("proof stat = %+v, want %+v", got, want)
	}
}
