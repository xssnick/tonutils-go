package funcs

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestStorageStatRawTraversalPreservesGasAndReadRecord(t *testing.T) {
	shared := cell.BeginCell().MustStoreUInt(0x11, 8).EndCell()
	unique := cell.BeginCell().MustStoreUInt(0x22, 8).EndCell()
	root := cell.BeginCell().
		MustStoreUInt(0x33, 8).
		MustStoreRef(shared).
		MustStoreRef(shared).
		MustStoreRef(unique).
		EndCell()
	read := cell.NewReadSet(root)

	state := vm.State{Gas: vm.GasWithLimit(10_000)}
	state.Cells.Init(&state)
	stat := newStorageStat(10, &state)
	ok, err := stat.addCell(read.Root())
	if err != nil || !ok {
		t.Fatalf("traversal failed: ok=%v err=%v", ok, err)
	}
	if stat.cells != 3 || stat.bits != 24 || stat.refs != 3 {
		t.Fatalf("unexpected storage stat: cells=%d bits=%d refs=%d", stat.cells, stat.bits, stat.refs)
	}
	if got := state.Gas.Used(); got != 3*vm.CellLoadGasPrice {
		t.Fatalf("gas used = %d, want %d", got, 3*vm.CellLoadGasPrice)
	}

	for _, visited := range []*cell.Cell{root, shared, unique} {
		if _, recorded := read.Contains(visited.HashKey()); !recorded {
			t.Fatalf("raw traversal did not record visited cell %x", visited.Hash())
		}
	}
	// The record is keyed by cell, not by reference, so the duplicated reference
	// cannot appear as a second entry. That the shared cell is counted once is the
	// same fact the gas assertion above pins: three loads for three distinct cells.
	if read.Size() != 3 {
		t.Fatalf("recorded %d cells, want the root, the shared cell once and the unique cell", read.Size())
	}
}
