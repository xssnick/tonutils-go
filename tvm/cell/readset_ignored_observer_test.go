package cell

import (
	"testing"
)

// The observer sees what an ignore scope drops and changes nothing else. A
// caller whose reads are premature rather than unwanted — one reading before a
// Merkle update exists, whose reads would otherwise widen the update's old side
// — keeps them here and hands them back through Record afterwards, so the
// proof still carries what the reader touched while the update does not.
func TestReadSetIgnoredObserverSeesTheDroppedReadsAndRecordsNone(t *testing.T) {
	first := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	second := BeginCell().MustStoreUInt(0x22, 8).EndCell()
	root := BeginCell().MustStoreUInt(1, 1).MustStoreRef(first).MustStoreRef(second).EndCell()

	rs := NewReadSet(root)
	slice, err := rs.Root().BeginParse()
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	refs := make([]*Cell, 0, 2)
	for i := range 2 {
		ref, refErr := slice.PeekRefCellAt(i)
		if refErr != nil {
			t.Fatalf("peek ref %d: %v", i, refErr)
		}
		refs = append(refs, ref)
	}

	var seen []*Cell
	rs.IgnoreReads(true)
	rs.SetIgnoredObserver(func(c *Cell) { seen = append(seen, c) })
	for i, ref := range refs {
		if _, err = ref.BeginParse(); err != nil {
			t.Fatalf("read ref %d: %v", i, err)
		}
	}
	rs.SetIgnoredObserver(nil)
	rs.IgnoreReads(false)

	if len(seen) != 2 {
		t.Fatalf("the observer saw %d cells, want the 2 the scope dropped", len(seen))
	}
	for _, hash := range []Hash{first.HashKey(), second.HashKey()} {
		if _, ok := rs.Contains(hash); ok {
			t.Fatalf("%x was recorded, but the scope exists to drop it", hash)
		}
	}

	// The point of keeping them: they can be recorded later, through the same
	// entry point OnLoad uses, without reading anything a second time.
	for _, c := range seen {
		rs.Record(c)
	}
	for _, hash := range []Hash{first.HashKey(), second.HashKey()} {
		if _, ok := rs.Contains(hash); !ok {
			t.Fatalf("%x was not recorded by handing the observed cell back", hash)
		}
	}
}

// A scope opened underneath the observer's is dropping reads for its own
// reason — a dictionary validating its root is the standing example — and those
// cells are not the installer's to keep. Firing on them would hand a caller
// cells it never asked to read.
func TestReadSetIgnoredObserverSkipsANestedScope(t *testing.T) {
	nested := BeginCell().MustStoreUInt(0x33, 8).EndCell()
	own := BeginCell().MustStoreUInt(0x44, 8).EndCell()
	root := BeginCell().MustStoreUInt(1, 1).MustStoreRef(nested).MustStoreRef(own).EndCell()

	rs := NewReadSet(root)
	slice, err := rs.Root().BeginParse()
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	nestedRef, err := slice.PeekRefCellAt(0)
	if err != nil {
		t.Fatalf("peek nested ref: %v", err)
	}
	ownRef, err := slice.PeekRefCellAt(1)
	if err != nil {
		t.Fatalf("peek own ref: %v", err)
	}

	var seen []Hash
	rs.IgnoreReads(true)
	rs.SetIgnoredObserver(func(c *Cell) { seen = append(seen, c.HashKey()) })

	rs.IgnoreReads(true)
	if _, err = nestedRef.BeginParse(); err != nil {
		t.Fatalf("read inside the nested scope: %v", err)
	}
	rs.IgnoreReads(false)

	if _, err = ownRef.BeginParse(); err != nil {
		t.Fatalf("read inside the observer's own scope: %v", err)
	}

	rs.SetIgnoredObserver(nil)
	rs.IgnoreReads(false)

	if len(seen) != 1 || seen[0] != own.HashKey() {
		t.Fatalf("the observer saw %d cells %x, want only the one read at its own depth %x",
			len(seen), seen, own.HashKey())
	}
}

// Without an observer the recorder behaves exactly as it did before there was
// one: an ignore scope still drops every read and tells nobody.
func TestReadSetIgnoreScopeWithoutAnObserverIsUnchanged(t *testing.T) {
	child := BeginCell().MustStoreUInt(0x55, 8).EndCell()
	root := BeginCell().MustStoreUInt(1, 1).MustStoreRef(child).EndCell()

	rs := NewReadSet(root)
	slice, err := rs.Root().BeginParse()
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	ref, err := slice.PeekRefCellAt(0)
	if err != nil {
		t.Fatalf("peek ref: %v", err)
	}

	rs.IgnoreReads(true)
	if _, err = ref.BeginParse(); err != nil {
		t.Fatalf("read: %v", err)
	}
	rs.IgnoreReads(false)

	if _, ok := rs.Contains(child.HashKey()); ok {
		t.Fatal("an ignored read was recorded")
	}
	if _, ok := rs.Contains(root.HashKey()); !ok {
		t.Fatal("the read outside the scope was dropped too")
	}
}
