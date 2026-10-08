package cell

import "testing"

// Detach is the half of Seal a producer needs partway through finishing a block:
// the reading is over, but the record still has to be consulted to select the
// proof. If a later edit makes Detach drop the table the way Seal does, that
// selection stops finding the cells it recorded and resolves each one again —
// from disk, on the hot path, which is the one thing this collator may not do.
func TestDetachKeepsTheTableAndStopsRecording(t *testing.T) {
	source := BeginCell().
		MustStoreUInt(1, 8).
		MustStoreRef(BeginCell().MustStoreUInt(2, 8).
			MustStoreRef(BeginCell().MustStoreUInt(3, 8).EndCell()).EndCell()).
		EndCell()

	set := NewReadSet(source)
	slice, err := set.Root().BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = slice.LoadUInt(8); err != nil {
		t.Fatal(err)
	}
	child, err := slice.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = child.LoadUInt(8); err != nil {
		t.Fatal(err)
	}
	recorded := set.Size()
	if recorded < 2 {
		t.Fatalf("the fixture recorded %d cells; it cannot show anything", recorded)
	}

	set.Detach()

	if !set.Inert() {
		t.Error("a detached set is not inert")
	}
	if set.Sealed() {
		t.Error("Detach sealed the set; the record must survive it")
	}
	if got := set.Size(); got != recorded {
		t.Errorf("the record holds %d cells after Detach, want the %d it had", got, recorded)
	}
	if set.ChildTrace(0) != nil {
		t.Error("a detached set still hands out its trace; descents through cells it " +
			"already handed out would keep recording")
	}

	// A descent through a cell handed out while the set was open must now record
	// nothing — this is the fence itself.
	deeper, err := child.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = deeper.LoadUInt(8); err != nil {
		t.Fatal(err)
	}
	if got := set.Size(); got != recorded {
		t.Errorf("a descent after Detach recorded %d more cells", got-recorded)
	}

	// The explicit write paths are fenced too. The trace fence above already
	// stops a descent from reaching them, so without this the gate on record and
	// RecordUnbilled would be untested — and those are the paths a cache or the
	// VM uses to hand the set a cell it took out of the source tree itself,
	// which is exactly what a successor collation running beside us would do.
	fresh := BeginCell().MustStoreUInt(9, 8).EndCell()
	set.Record(fresh)
	set.RecordUnbilled(fresh)
	if got := set.Size(); got != recorded {
		t.Errorf("an explicit Record after Detach added %d cells", got-recorded)
	}

	// And Seal still does what it always did.
	set.Seal()
	if !set.Sealed() || !set.Inert() {
		t.Error("Seal after Detach did not seal")
	}
	if got := set.Size(); got != 0 {
		t.Errorf("Seal left %d cells in the table", got)
	}
}
