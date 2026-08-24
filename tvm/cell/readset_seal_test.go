package cell

import (
	"runtime"
	"testing"
	"time"
)

// Seal is the point after which the recorder is inert, and the cells it handed
// out are what make that a question at all: they keep pointing at its trace, so
// anything that descends through one afterwards still reaches OnLoad. The ignore
// branch there is the one path that runs before the sealed check, so an observer
// left installed keeps being called by a caller that has long since finished —
// and is called with cells belonging to whatever is reading now, not to the
// collection it was filling.
func TestReadSetSealStopsTheIgnoredObserverFiring(t *testing.T) {
	child := BeginCell().MustStoreUInt(0x66, 8).EndCell()
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

	fired := 0
	rs.IgnoreReads(true)
	rs.SetIgnoredObserver(func(*Cell) { fired++ })
	if _, err = ref.BeginParse(); err != nil {
		t.Fatalf("read inside the scope: %v", err)
	}
	if fired != 1 {
		t.Fatalf("the observer saw %d reads before the seal, want the 1 the scope dropped", fired)
	}

	// The scope is left open on purpose. Sealing with one open is exactly the
	// case the guard is for: a caller that unwound its scope had no observer
	// left to drop.
	rs.Seal()

	if _, err = ref.BeginParse(); err != nil {
		t.Fatalf("read a retained cell after the seal: %v", err)
	}
	if fired != 1 {
		t.Fatalf("the observer fired %d times, so a sealed recorder is still calling into "+
			"the caller that closed it", fired)
	}
}

// The other half of the same line: the observer is a closure over the caller's
// world — in a collation it is bound to the collation itself — so a recorder
// that keeps it keeps that world alive for as long as anything retains the
// recorder, which is what sealing exists to end.
func TestReadSetSealReleasesTheIgnoredObserver(t *testing.T) {
	rs := NewReadSet(BeginCell().MustStoreUInt(1, 1).EndCell())
	// The recorder must outlive the check: a released observer proves nothing if
	// the recorder holding it was collected too.
	defer runtime.KeepAlive(rs)

	released := make(chan struct{})
	// Installed inside a function so that the state the closure captures has no
	// other reference on the stack of the test itself.
	func() {
		state := new([64]byte)
		runtime.SetFinalizer(state, func(*[64]byte) { close(released) })
		rs.IgnoreReads(true)
		rs.SetIgnoredObserver(func(*Cell) { state[0]++ })
	}()

	rs.Seal()

	deadline := time.Now().Add(30 * time.Second)
	for {
		runtime.GC()
		select {
		case <-released:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the observer closure is still reachable from the sealed recorder, " +
				"so everything it closes over is retained for as long as the recorder is")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
