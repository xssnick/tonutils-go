package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestBlockContextComputeAccountStorageStat(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(0xa5, 8).EndCell()
	storage := cell.BeginCell().MustStoreUInt(7, 64).MustStoreRef(leaf).EndCell()
	account := &PreparedAccount{runtime: transactionRuntimeAccount{storageCell: storage}}
	block, err := emptyPreparedTestConfig().NewBlockContext(BlockOptions{Now: 1})
	if err != nil {
		t.Fatal(err)
	}

	root, err := block.ComputeAccountStorageStat(account)
	if err != nil {
		t.Fatal(err)
	}
	_, expected, err := transactionComputeAccountStorageStat(storage)
	if err != nil {
		t.Fatal(err)
	}
	if root.HashKey() != expected.HashKey() {
		t.Fatalf("storage stat root = %x, want %x", root.HashKey(), expected.HashKey())
	}
}
