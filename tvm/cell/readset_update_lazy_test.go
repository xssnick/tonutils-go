package cell

import "testing"

// countingLazyLoader resolves a lazy tree and counts the resolutions per cell,
// so a test can say which subtree was fetched rather than only how many were.
type countingLazyLoader struct {
	cells map[Hash]*Cell
	calls map[Hash]int
}

func (l *countingLazyLoader) LoadCell(hash Hash) (*Cell, error) {
	if l.calls == nil {
		l.calls = map[Hash]int{}
	}
	l.calls[hash]++
	return l.cells[hash], nil
}

// TestReadSetMerkleUpdateDoesNotLoadUnreadBoundary pins the one asymmetry the
// childless exception has to observe.
//
// Leaving a childless cell out of the pruning is a size trade: the boundary
// would weigh more than the cell, so the update is smaller with the cell in it.
// Making that trade means knowing the cell has no children, and an unread
// reference into a disk-backed predecessor cannot be asked — it reports no
// references because it is a stand-in, not because the subtree is empty. Asking
// anyway turns a byte-counting optimisation into a disk read for every untouched
// boundary the destination kept, which is the whole traffic an update is
// supposed to avoid.
//
// The block below is one collation in miniature: one subtree rebuilt, one left
// exactly as the predecessor had it. The untouched one must survive the update
// without ever being fetched.
func TestReadSetMerkleUpdateDoesNotLoadUnreadBoundary(t *testing.T) {
	childless := BeginCell().MustStoreUInt(0xA5A5, 16).EndCell()
	leaf := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	branch := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(leaf).EndCell()
	resident := BeginCell().MustStoreUInt(0xdead, 16).
		MustStoreRef(childless).
		MustStoreRef(branch).
		EndCell()

	childlessHash := childless.HashKey()
	loader := &countingLazyLoader{cells: map[Hash]*Cell{
		childlessHash:    childless,
		branch.HashKey(): branch,
	}}

	dsc1, dsc2 := resident.descriptors(resident.getLevelMask())
	lazyRoot := mustCreateWithLazyRefsUnsafe(t,
		uint16(dsc1)<<8|uint16(dsc2),
		serializedCellData(resident),
		significantHashes(resident),
		significantDepths(resident),
		[]LazyRef{lazyRefFromCell(childless), lazyRefFromCell(branch)},
		loader.LoadCell,
	)
	if lazyRoot.HashKey() != resident.HashKey() {
		t.Fatal("lazified root does not stand for the resident one")
	}

	// Read like a collation does: touch the root, descend into the subtree that
	// is about to change, and never look at the other one.
	rs := NewReadSet(lazyRoot)
	rootSlice, err := rs.Root().BeginParse()
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	tag, err := rootSlice.LoadUInt(16)
	if err != nil {
		t.Fatalf("load root tag: %v", err)
	}
	untouched, err := rootSlice.LoadRefCell()
	if err != nil {
		t.Fatalf("take the untouched ref: %v", err)
	}
	branchRef, err := rootSlice.LoadRefCell()
	if err != nil {
		t.Fatalf("take the branch ref: %v", err)
	}
	branchSlice, err := branchRef.BeginParse()
	if err != nil {
		t.Fatalf("parse branch: %v", err)
	}
	branchTag, err := branchSlice.LoadUInt(8)
	if err != nil {
		t.Fatalf("load branch tag: %v", err)
	}

	if !untouched.IsLazy() {
		t.Fatal("the untouched reference was materialized by the read phase")
	}
	if got := loader.calls[childlessHash]; got != 0 {
		t.Fatalf("the read phase fetched the untouched subtree %d times", got)
	}
	if _, read := rs.Contains(childlessHash); read {
		t.Fatal("the untouched subtree ended up in the read set")
	}

	// The destination keeps the untouched reference exactly as it arrived and
	// rebuilds only what was read.
	rebuilt := BeginCell().MustStoreUInt(branchTag+1, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0x99, 8).EndCell()).
		EndCell()
	to := BeginCell().MustStoreUInt(tag, 16).
		MustStoreRef(untouched).
		MustStoreRef(rebuilt).
		EndCell()

	update, err := rs.CreateMerkleUpdate(to)
	if err != nil {
		t.Fatalf("create merkle update: %v", err)
	}

	if got := loader.calls[childlessHash]; got != 0 {
		t.Fatalf("building the update fetched the untouched childless subtree %d times, want 0", got)
	}

	// Pruning it must still leave an update that means what it says: it has to
	// apply to the real predecessor and rebuild the destination that was
	// assembled here.
	if err = ValidateMerkleUpdate(update); err != nil {
		t.Fatalf("validate update: %v", err)
	}
	applied, err := ApplyMerkleUpdate(resident, update)
	if err != nil {
		t.Fatalf("apply update to the resident predecessor: %v", err)
	}
	if applied.HashKey() != to.HashKey() {
		t.Fatalf("applying the update rebuilt a different destination:\n got  %x\n want %x",
			applied.HashKey(), to.HashKey())
	}
}

// TestReadSetMerkleUpdateKeepsResidentChildlessCell is the other half of the
// asymmetry: with the predecessor in memory the childless exception costs
// nothing to evaluate, so it still applies and the cell stays ordinary. This is
// what makes the change above provably invisible to a resident collation.
func TestReadSetMerkleUpdateKeepsResidentChildlessCell(t *testing.T) {
	childless := BeginCell().MustStoreUInt(0xA5A5, 16).EndCell()
	leaf := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	branch := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(leaf).EndCell()
	resident := BeginCell().MustStoreUInt(0xdead, 16).
		MustStoreRef(childless).
		MustStoreRef(branch).
		EndCell()

	rs := NewReadSet(resident)
	rootSlice, err := rs.Root().BeginParse()
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	tag, err := rootSlice.LoadUInt(16)
	if err != nil {
		t.Fatalf("load root tag: %v", err)
	}
	untouched, err := rootSlice.LoadRefCell()
	if err != nil {
		t.Fatalf("take the untouched ref: %v", err)
	}
	branchRef, err := rootSlice.LoadRefCell()
	if err != nil {
		t.Fatalf("take the branch ref: %v", err)
	}
	branchSlice, err := branchRef.BeginParse()
	if err != nil {
		t.Fatalf("parse branch: %v", err)
	}
	branchTag, err := branchSlice.LoadUInt(8)
	if err != nil {
		t.Fatalf("load branch tag: %v", err)
	}

	rebuilt := BeginCell().MustStoreUInt(branchTag+1, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0x99, 8).EndCell()).
		EndCell()
	to := BeginCell().MustStoreUInt(tag, 16).
		MustStoreRef(untouched).
		MustStoreRef(rebuilt).
		EndCell()

	update, err := rs.CreateMerkleUpdate(to)
	if err != nil {
		t.Fatalf("create merkle update: %v", err)
	}
	if err = ValidateMerkleUpdate(update); err != nil {
		t.Fatalf("validate update: %v", err)
	}

	updateTo, err := update.PeekRef(1)
	if err != nil {
		t.Fatalf("peek destination half: %v", err)
	}
	toSlice, err := updateTo.BeginParse()
	if err != nil {
		t.Fatalf("parse destination half: %v", err)
	}
	if _, err = toSlice.LoadUInt(16); err != nil {
		t.Fatalf("load destination tag: %v", err)
	}
	kept, err := toSlice.LoadRefCell()
	if err != nil {
		t.Fatalf("take the untouched ref from the update: %v", err)
	}
	if kept.GetType() == PrunedCellType {
		t.Fatal("a resident childless cell was replaced by a boundary")
	}
	if kept.HashKey() != childless.HashKey() {
		t.Fatal("the update carries a different cell in the untouched position")
	}

	applied, err := ApplyMerkleUpdate(resident, update)
	if err != nil {
		t.Fatalf("apply update: %v", err)
	}
	if applied.HashKey() != to.HashKey() {
		t.Fatal("applying the update rebuilt a different destination")
	}
}
