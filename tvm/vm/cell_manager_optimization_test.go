package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestCellLoadSetInlineSpillAndExactEquality(t *testing.T) {
	var set cellLoadSet
	var keys [cellLoadInlineCapacity + 2]cell.Hash
	for i := range keys {
		keys[i][0] = byte(i + 1)
		keys[i][31] = byte(0xF0 + i)
		if !set.add(keys[i]) {
			t.Fatalf("key %d was reported as duplicate", i)
		}
		if !set.contains(keys[i]) {
			t.Fatalf("key %d was not retained", i)
		}
	}
	if set.spill == nil {
		t.Fatal("set did not spill after exhausting inline storage")
	}
	for i := range keys {
		if set.add(keys[i]) {
			t.Fatalf("duplicate key %d was inserted twice", i)
		}
	}

	nearCollision := keys[0]
	nearCollision[31]++
	if set.contains(nearCollision) {
		t.Fatal("set matched a key without exact hash equality")
	}
}

func TestCellManagerInitPreservesLoadedCells(t *testing.T) {
	state := State{Gas: GasWithLimit(10_000)}
	state.Cells.Init(&state)
	key := cell.Hash{0xAA}
	if err := state.Cells.RegisterCellLoadKey(key); err != nil {
		t.Fatal(err)
	}

	state.Cells.Init(&state)
	if err := state.Cells.RegisterCellLoadKey(key); err != nil {
		t.Fatal(err)
	}
	if got := state.Gas.Used(); got != CellLoadGasPrice+CellReloadGasPrice {
		t.Fatalf("gas after re-init = %d, want %d", got, CellLoadGasPrice+CellReloadGasPrice)
	}
}

func TestCellManagerLoadRefKeepsRawCellAndUsageTrace(t *testing.T) {
	child := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	root := cell.BeginCell().MustStoreRef(child).EndCell()
	usage := cell.NewCellUsageTree()
	tracedRoot := root.WithTrace(usage.RootTrace())

	state := State{Gas: GasWithLimit(10_000)}
	state.Cells.Init(&state)
	parsed, err := state.Cells.BeginParse(tracedRoot)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := state.Cells.LoadRef(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RawCell() != child {
		t.Fatal("LoadRef materialized a traced Cell wrapper")
	}
	if loaded.Trace() == nil {
		t.Fatal("LoadRef lost the effective child trace")
	}
	node, ok := usage.NodeForTrace(loaded.Trace())
	if !ok || !usage.IsLoaded(node) {
		t.Fatal("LoadRef did not mark the child usage-tree node as loaded")
	}
	if got := state.Gas.Used(); got != 2*CellLoadGasPrice {
		t.Fatalf("gas used = %d, want %d", got, 2*CellLoadGasPrice)
	}
}

func TestCellManagerLoadRefRawGatewayPreservesLazyLoading(t *testing.T) {
	child := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	root := cell.BeginCell().MustStoreRef(child).EndCell()
	lazyRoot := makeLazyLibraryRoot(t, root)

	state := State{Gas: GasWithLimit(10_000)}
	state.Cells.Init(&state)
	parsed, err := state.Cells.BeginParse(lazyRoot)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := state.Cells.LoadRef(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RawCell().IsLazy() {
		t.Fatal("LoadRef left the lazy boundary unresolved")
	}
	if got, err := loaded.LoadUInt(8); err != nil || got != 0xAB {
		t.Fatalf("loaded lazy child = (%x, %v), want (ab, nil)", got, err)
	}
	if got := state.Gas.Used(); got != 2*CellLoadGasPrice {
		t.Fatalf("lazy load gas = %d, want %d", got, 2*CellLoadGasPrice)
	}
}
