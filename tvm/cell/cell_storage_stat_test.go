package cell

import "testing"

func TestCellStorageStatCountsCellsProofsAndSharedRefs(t *testing.T) {
	tree := NewCellUsageTree()
	externalNode := tree.CreateChild(tree.RootNode(), 0)

	external := BeginCell().MustStoreUInt(0xa, 4).EndCell().WithTrace(tree.Trace(externalNode))
	shared := BeginCell().MustStoreUInt(0xcc, 8).EndCell()
	root := BeginCell().
		MustStoreUInt(0xdd, 8).
		MustStoreRef(external).
		MustStoreRef(shared).
		MustStoreRef(shared).
		EndCell()

	stat := NewCellStorageStat()
	if err := stat.AddCell(root); err != nil {
		t.Fatal(err)
	}
	if err := stat.AddProof(root, tree); err != nil {
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
	if err := stat.AddProof(nil, NewCellUsageTree()); err != nil {
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
