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

// The load observer and the first-load gas price are the same event seen twice,
// so the invariant is stated where it is true rather than in the consumer: a
// first load hands the cell to the observer and costs CellLoadGasPrice, a repeat
// hands over nothing and costs CellReloadGasPrice. A proof recorder is attached
// here, which is why a missed first load matters (a hole in the proof) and a
// duplicate report does not (a set insert), and why the fire condition may never
// drift away from the price condition.
func TestCellManagerOnCellLoadFiresOncePerCellWithLoadPrice(t *testing.T) {
	first := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	second := cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell()

	var observed []*cell.Cell
	state := State{Gas: GasWithLimit(10_000)}
	state.OnCellLoad = func(c *cell.Cell) { observed = append(observed, c) }
	state.Cells.Init(&state)

	if err := state.Cells.RegisterCellLoad(first); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 1 || observed[0] != first {
		t.Fatalf("first load observed %d cells, want the loaded cell itself", len(observed))
	}
	if got := state.Gas.Used(); got != CellLoadGasPrice {
		t.Fatalf("first load gas = %d, want %d", got, CellLoadGasPrice)
	}

	if err := state.Cells.RegisterCellLoad(first); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 1 {
		t.Fatalf("repeat load observed %d cells, want the observer to stay silent", len(observed))
	}
	if got := state.Gas.Used(); got != CellLoadGasPrice+CellReloadGasPrice {
		t.Fatalf("repeat load gas = %d, want %d", got, CellLoadGasPrice+CellReloadGasPrice)
	}

	if err := state.Cells.RegisterCellLoad(second); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 || observed[1] != second {
		t.Fatalf("distinct load observed %d cells, want the second cell reported", len(observed))
	}
	if got := state.Gas.Used(); got != 2*CellLoadGasPrice+CellReloadGasPrice {
		t.Fatalf("distinct load gas = %d, want %d", got, 2*CellLoadGasPrice+CellReloadGasPrice)
	}

	// A nil cell is neither charged nor reported: the recorder must never be
	// handed something it would have to guard against itself.
	if err := state.Cells.RegisterCellLoad(nil); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 {
		t.Fatalf("nil load observed %d cells, want none", len(observed))
	}
	if got := state.Gas.Used(); got != 2*CellLoadGasPrice+CellReloadGasPrice {
		t.Fatalf("nil load gas = %d, want it unchanged at %d", got, 2*CellLoadGasPrice+CellReloadGasPrice)
	}
}

// The same invariant through the parsing gateway the VM actually uses, because
// that is where a cell reaches the observer in production: descending a
// reference reports the child once, and re-parsing the same reference reports
// nothing while still charging the reload.
func TestCellManagerOnCellLoadFiresOncePerCellThroughBeginParse(t *testing.T) {
	child := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	root := cell.BeginCell().MustStoreRef(child).EndCell()

	observed := map[cell.Hash]int{}
	state := State{Gas: GasWithLimit(10_000)}
	state.OnCellLoad = func(c *cell.Cell) { observed[c.HashKey()]++ }
	state.Cells.Init(&state)

	parsed, err := state.Cells.BeginParse(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.Cells.LoadRef(parsed); err != nil {
		t.Fatal(err)
	}
	if observed[root.HashKey()] != 1 || observed[child.HashKey()] != 1 {
		t.Fatalf("root/child reported %d/%d times, want once each",
			observed[root.HashKey()], observed[child.HashKey()])
	}
	if got := state.Gas.Used(); got != 2*CellLoadGasPrice {
		t.Fatalf("gas after two first loads = %d, want %d", got, 2*CellLoadGasPrice)
	}

	reparsed, err := state.Cells.BeginParse(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.Cells.LoadRef(reparsed); err != nil {
		t.Fatal(err)
	}
	if observed[root.HashKey()] != 1 || observed[child.HashKey()] != 1 {
		t.Fatalf("re-parse reported root/child %d/%d times, want the counts unchanged at 1/1",
			observed[root.HashKey()], observed[child.HashKey()])
	}
	if got := state.Gas.Used(); got != 2*CellLoadGasPrice+2*CellReloadGasPrice {
		t.Fatalf("gas after two reloads = %d, want %d", got, 2*CellLoadGasPrice+2*CellReloadGasPrice)
	}
}

func TestCellManagerLoadRefKeepsRawCellAndRecordsRead(t *testing.T) {
	child := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	root := cell.BeginCell().MustStoreRef(child).EndCell()
	read := cell.NewReadSet(root)
	tracedRoot := read.Root()

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
	// The recorder is keyed by hash rather than by trace identity, so the question
	// the node lookup answered — did the read reach the child through this trace —
	// is now asked of the record directly.
	if _, recorded := read.Contains(child.HashKey()); !recorded {
		t.Fatal("LoadRef did not record the child read")
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
