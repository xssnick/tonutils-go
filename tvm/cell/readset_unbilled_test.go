package cell

import "testing"

// A cell can reach the read set by two routes: a traced read of the source tree,
// which bills it to the collated-size estimate through onRecord, and the VM's
// own loaded-cell report through RecordUnbilled, which does not — the VM also
// reports cells that are not in the tree at all. The two routes meet on the
// same cells, in either order, and the size estimate must come out the same:
// every cell the tree read sees is billed exactly once, and a cell only the VM
// saw is never billed.
func TestReadSetBillsACellOnceRegardlessOfWhichRouteSawItFirst(t *testing.T) {
	a := BeginCell().MustStoreUInt(1, 8).EndCell()
	b := BeginCell().MustStoreUInt(2, 8).EndCell()
	onlyVM := BeginCell().MustStoreUInt(3, 8).EndCell()

	set := NewReadSet(nil)
	billed := map[Hash]int{}
	set.SetRecordCallback(func(c *Cell) { billed[c.HashKey()]++ })

	// Traced read first, then the VM report: billed on the read, silent after.
	set.Record(a)
	set.RecordUnbilled(a)
	set.Record(a)

	// The VM report first, then the traced read: the read must still bill it.
	// This is the order an account's code and data arrive in — the machine
	// loads them before the tree walk that records them for the proof.
	set.RecordUnbilled(b)
	set.Record(b)
	set.Record(b)
	set.RecordUnbilled(b)

	// A cell only the VM reported is in the set but never billed.
	set.RecordUnbilled(onlyVM)
	set.RecordUnbilled(onlyVM)

	if got := billed[a.HashKey()]; got != 1 {
		t.Errorf("cell read first was billed %d times, want 1", got)
	}
	if got := billed[b.HashKey()]; got != 1 {
		t.Errorf("cell reported by the VM first was billed %d times, want 1", got)
	}
	if got := billed[onlyVM.HashKey()]; got != 0 {
		t.Errorf("cell only the VM reported was billed %d times, want 0", got)
	}
	if got := set.Size(); got != 3 {
		t.Errorf("set holds %d cells, want 3", got)
	}
	for _, c := range []*Cell{a, b, onlyVM} {
		if set.RecordedCell(c.HashKey()) == nil {
			t.Errorf("cell %x is not in the set", c.HashKey())
		}
	}
}
