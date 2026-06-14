package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionAccountStorageStatReplaceSharedRef(t *testing.T) {
	shared := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	oldStorage := cell.BeginCell().MustStoreUInt(0xA, 4).
		MustStoreRef(shared).
		MustStoreRef(shared).
		EndCell()
	newStorage := cell.BeginCell().MustStoreUInt(0xB, 4).
		MustStoreRef(shared).
		EndCell()

	oldUsage, oldDict, err := transactionComputeAccountStorageStat(oldStorage)
	if err != nil {
		t.Fatal(err)
	}
	if oldUsage != (transactionUsage{cells: 2, bits: 12}) {
		t.Fatalf("old storage usage = %+v, want 2 cells and 12 bits", oldUsage)
	}

	stat, err := transactionInitAccountStorageStat(oldDict, oldStorage, tlb.StorageUsed{
		CellsUsed: new(big.Int).SetUint64(oldUsage.cells),
		BitsUsed:  new(big.Int).SetUint64(oldUsage.bits),
	}, transactionAccountStorageStatRootHash(oldDict))
	if err != nil {
		t.Fatal(err)
	}

	gotUsage, gotDict, err := stat.replaceStorage(newStorage)
	if err != nil {
		t.Fatal(err)
	}

	wantUsage, wantDict, err := transactionComputeAccountStorageStat(newStorage)
	if err != nil {
		t.Fatal(err)
	}
	if gotUsage != wantUsage {
		t.Fatalf("replace storage usage = %+v, want %+v", gotUsage, wantUsage)
	}
	if !transactionCellEqual(gotDict, wantDict) {
		t.Fatal("replace storage stat dictionary does not match full recompute")
	}
}

func TestTransactionAccountStorageRefsUnchanged(t *testing.T) {
	left := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	right := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	oldStorage := cell.BeginCell().MustStoreUInt(0xA, 4).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()
	newStorage := cell.BeginCell().MustStoreUInt(0xB, 4).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()

	unchanged, err := transactionAccountStorageRefsUnchanged(oldStorage, newStorage)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged {
		t.Fatal("expected same storage refs to be unchanged")
	}

	reordered := cell.BeginCell().MustStoreUInt(0xC, 4).
		MustStoreRef(right).
		MustStoreRef(left).
		EndCell()
	unchanged, err = transactionAccountStorageRefsUnchanged(oldStorage, reordered)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged {
		t.Fatal("expected reordered storage refs to be changed")
	}
}

func TestTransactionAccountStorageRootDiffSortsByHash(t *testing.T) {
	keep := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	remove := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	add := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()

	oldRoots := [4]*cell.Cell{remove, keep}
	newRoots := [4]*cell.Cell{add, keep}
	toAdd, toAddNum, toDel, toDelNum := transactionAccountStorageRootDiff(oldRoots, 2, newRoots, 2)
	if toAddNum != 1 || toAdd[0].HashKey() != add.HashKey() {
		t.Fatalf("toAdd = %v, want only add root", toAddNum)
	}
	if toDelNum != 1 || toDel[0].HashKey() != remove.HashKey() {
		t.Fatalf("toDel = %v, want only remove root", toDelNum)
	}
}
