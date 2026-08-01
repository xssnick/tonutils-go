package funcs

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestStorageStatRawTraversalPreservesGasAndUsageTrace(t *testing.T) {
	shared := cell.BeginCell().MustStoreUInt(0x11, 8).EndCell()
	unique := cell.BeginCell().MustStoreUInt(0x22, 8).EndCell()
	root := cell.BeginCell().
		MustStoreUInt(0x33, 8).
		MustStoreRef(shared).
		MustStoreRef(shared).
		MustStoreRef(unique).
		EndCell()
	usage := cell.NewCellUsageTree()

	state := vm.State{Gas: vm.GasWithLimit(10_000)}
	state.Cells.Init(&state)
	stat := newStorageStat(10, &state)
	ok, err := stat.addCell(root.WithTrace(usage.RootTrace()))
	if err != nil || !ok {
		t.Fatalf("traversal failed: ok=%v err=%v", ok, err)
	}
	if stat.cells != 3 || stat.bits != 24 || stat.refs != 3 {
		t.Fatalf("unexpected storage stat: cells=%d bits=%d refs=%d", stat.cells, stat.bits, stat.refs)
	}
	if got := state.Gas.Used(); got != 3*vm.CellLoadGasPrice {
		t.Fatalf("gas used = %d, want %d", got, 3*vm.CellLoadGasPrice)
	}

	rootNode := usage.RootNode()
	firstShared := usage.GetChild(rootNode, 0)
	duplicateShared := usage.GetChild(rootNode, 1)
	uniqueNode := usage.GetChild(rootNode, 2)
	if !usage.IsLoaded(rootNode) || !usage.IsLoaded(firstShared) || !usage.IsLoaded(uniqueNode) {
		t.Fatal("raw traversal did not mark visited usage-tree nodes")
	}
	if duplicateShared == 0 || usage.IsLoaded(duplicateShared) {
		t.Fatal("duplicate hash should create traversal context but must not be loaded twice")
	}
}
