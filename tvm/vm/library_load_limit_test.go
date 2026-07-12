package vm

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// makeLibraryRootMulti builds a single library dictionary root cell holding
// all of the given library cells, keyed by their hash, mirroring the shape
// LoadLibraryByHash expects (see makeLibraryRoot in libraries_stack_more_test.go
// for the single-library variant).
func makeLibraryRootMulti(t *testing.T, libs ...*cell.Cell) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(256)
	for _, lib := range libs {
		key := cell.BeginCell().MustStoreSlice(lib.Hash(), 256).EndCell()
		value := cell.BeginCell().MustStoreRef(lib).EndCell()
		if err := dict.Set(key, value); err != nil {
			t.Fatalf("set library dict value: %v", err)
		}
	}
	root, err := dict.ToCell()
	if err != nil {
		t.Fatalf("dict to cell: %v", err)
	}
	return root
}

// distinctLibs returns n cells whose contents (and hence hashes) are all
// distinct, so each one occupies its own "unique hash" slot under the
// library load limit.
func distinctLibs(n int) []*cell.Cell {
	libs := make([]*cell.Cell, n)
	for i := 0; i < n; i++ {
		libs[i] = cell.BeginCell().MustStoreUInt(uint64(0x100+i), 16).EndCell()
	}
	return libs
}

func newLibraryLimitState(t *testing.T, libs ...*cell.Cell) *State {
	t.Helper()

	st := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(1_000_000), nil, tuple.Tuple{}, NewStack())
	st.InitForExecution()
	if len(libs) > 0 {
		st.SetLibraries(makeLibraryRootMulti(t, libs...))
	}
	return st
}

// Rule 1 & 3: unique hashes are counted, and once the configured limit of
// distinct hashes has been reached, a request for one more new hash is
// refused (nil, nil - the same shape callers already treat as "not found").
func TestLoadLibraryByHashEnforcesMaxLoads(t *testing.T) {
	libs := distinctLibs(3)
	st := newLibraryLimitState(t, libs...)
	st.SetMaxLibraryLoads(2)

	for i, lib := range libs[:2] {
		got, err := st.LoadLibraryByHash(lib.Hash())
		if err != nil {
			t.Fatalf("load library %d: %v", i, err)
		}
		if got == nil || got.HashKey() != lib.HashKey() {
			t.Fatalf("load library %d = %v, want %x", i, got, lib.Hash())
		}
	}

	// third distinct hash: limit already reached, must be refused.
	got, err := st.LoadLibraryByHash(libs[2].Hash())
	if err != nil {
		t.Fatalf("load library beyond limit: %v", err)
	}
	if got != nil {
		t.Fatalf("load library beyond limit = %v, want nil (refused)", got)
	}
}

// Rule "repeat is always free": once the limit is reached, re-requesting an
// already-loaded hash must still succeed - only *new* hashes are refused.
func TestLoadLibraryByHashRepeatAtLimitIsFree(t *testing.T) {
	libs := distinctLibs(2)
	st := newLibraryLimitState(t, libs...)
	st.SetMaxLibraryLoads(2)

	for i, lib := range libs {
		if got, err := st.LoadLibraryByHash(lib.Hash()); err != nil || got == nil || got.HashKey() != lib.HashKey() {
			t.Fatalf("initial load %d = (%v, %v), want (%x, nil)", i, got, err, lib.Hash())
		}
	}

	// at the limit now; re-requesting either already-seen hash must be free.
	for i, lib := range libs {
		got, err := st.LoadLibraryByHash(lib.Hash())
		if err != nil {
			t.Fatalf("repeat load %d: %v", i, err)
		}
		if got == nil || got.HashKey() != lib.HashKey() {
			t.Fatalf("repeat load %d = %v, want cached %x", i, got, lib.Hash())
		}
	}

	// a genuinely new hash must still be refused.
	third := cell.BeginCell().MustStoreUInt(0xABCD, 16).EndCell()
	if got, err := st.LoadLibraryByHash(third.Hash()); err != nil || got != nil {
		t.Fatalf("new hash at limit = (%v, %v), want (nil, nil)", got, err)
	}
}

// Rule 2, the easiest one to get backwards: the slot is consumed on the
// *attempt* itself, even when the subsequent lookup fails to find the hash
// in any registered library collection. We prove this by burning the whole
// limit on hashes that are not registered anywhere, then showing that a
// hash which *is* registered and would otherwise load successfully is still
// refused, because no slots remain.
func TestLoadLibraryByHashConsumesSlotOnFailedLookup(t *testing.T) {
	real := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	st := newLibraryLimitState(t, real)
	st.SetMaxLibraryLoads(2)

	missingA := bytes.Repeat([]byte{0x11}, 32)
	missingB := bytes.Repeat([]byte{0x22}, 32)

	if got, err := st.LoadLibraryByHash(missingA); err != nil || got != nil {
		t.Fatalf("load missing A = (%v, %v), want (nil, nil)", got, err)
	}
	if got, err := st.LoadLibraryByHash(missingB); err != nil || got != nil {
		t.Fatalf("load missing B = (%v, %v), want (nil, nil)", got, err)
	}

	if n := len(st.loadedLibraries); n != 2 {
		t.Fatalf("distinct attempted hashes = %d, want 2 (failed lookups must still consume a slot)", n)
	}
	var keyA, keyB cell.Hash
	copy(keyA[:], missingA)
	copy(keyB[:], missingB)
	if _, ok := st.loadedLibraries[keyA]; !ok {
		t.Fatal("missing hash A should be recorded as an attempted load")
	}
	if _, ok := st.loadedLibraries[keyB]; !ok {
		t.Fatal("missing hash B should be recorded as an attempted load")
	}

	// the limit (2) is now exhausted purely by failed lookups; a real,
	// registered library must still be refused.
	got, err := st.LoadLibraryByHash(real.Hash())
	if err != nil {
		t.Fatalf("load real library after limit exhausted by failures: %v", err)
	}
	if got != nil {
		t.Fatalf("load real library after limit exhausted by failures = %v, want nil (refused)", got)
	}
}

// Control for the above: with a high-enough limit the same registered
// library loads fine after the same two failed lookups, confirming the
// refusal above is really about the exhausted limit and not some other bug.
func TestLoadLibraryByHashSucceedsWhenLimitNotExhausted(t *testing.T) {
	real := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	st := newLibraryLimitState(t, real)
	st.SetMaxLibraryLoads(3)

	if got, _ := st.LoadLibraryByHash(bytes.Repeat([]byte{0x11}, 32)); got != nil {
		t.Fatal("expected missing lookup to return nil")
	}
	if got, _ := st.LoadLibraryByHash(bytes.Repeat([]byte{0x22}, 32)); got != nil {
		t.Fatal("expected missing lookup to return nil")
	}

	got, err := st.LoadLibraryByHash(real.Hash())
	if err != nil {
		t.Fatalf("load real library: %v", err)
	}
	if got == nil || got.HashKey() != real.HashKey() {
		t.Fatalf("load real library = %v, want %x", got, real.Hash())
	}
}

// Rule 4: an unset limit means unlimited, zero-overhead behaviour - many
// distinct registered libraries can all be loaded.
func TestLoadLibraryByHashUnsetLimitIsUnlimited(t *testing.T) {
	libs := distinctLibs(8)
	st := newLibraryLimitState(t, libs...)
	// SetMaxLibraryLoads intentionally never called.

	if st.hasMaxLibraryLoads {
		t.Fatal("hasMaxLibraryLoads should default to false")
	}

	for i, lib := range libs {
		got, err := st.LoadLibraryByHash(lib.Hash())
		if err != nil {
			t.Fatalf("load library %d: %v", i, err)
		}
		if got == nil || got.HashKey() != lib.HashKey() {
			t.Fatalf("load library %d = %v, want %x", i, got, lib.Hash())
		}
	}
	if st.loadedLibraries != nil {
		t.Fatalf("loadedLibraries should stay nil (no bookkeeping) when the limit is unset, got %d entries", len(st.loadedLibraries))
	}
}

// RUNVM child-state propagation: prepareChildForRun must share the same
// loadedLibraries map reference with the child (not a copy), so slots
// consumed by either parent or child are visible to the other.
func TestLibraryLoadLimitSharedAcrossRunChild(t *testing.T) {
	libs := distinctLibs(3)
	root := makeLibraryRootMulti(t, libs...)

	parent := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(1_000_000), nil, tuple.Tuple{}, NewStack())
	parent.InitForExecution()
	parent.SetLibraries(root)
	parent.SetMaxLibraryLoads(2)

	// parent consumes the first of two slots before running the child.
	if got, err := parent.LoadLibraryByHash(libs[0].Hash()); err != nil || got == nil || got.HashKey() != libs[0].HashKey() {
		t.Fatalf("parent preload = (%v, %v), want (%x, nil)", got, err, libs[0].Hash())
	}

	child := NewExecutionState(0, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
	child.CurrentCode = cell.BeginCell().MustStoreUInt(0xEF, 8).EndCell().MustBeginParse()

	parent.SetChildRunner(func(child *State) (int64, error) {
		// child should inherit the limit configuration.
		if !child.hasMaxLibraryLoads || child.maxLibraryLoads != 2 {
			t.Fatalf("child limit config = (%v, %d), want (true, 2)", child.hasMaxLibraryLoads, child.maxLibraryLoads)
		}
		// second (last remaining) slot, consumed from the child side.
		got, err := child.LoadLibraryByHash(libs[1].Hash())
		if err != nil {
			t.Fatalf("child load second library: %v", err)
		}
		if got == nil || got.HashKey() != libs[1].HashKey() {
			t.Fatalf("child load second library = %v, want %x", got, libs[1].Hash())
		}
		// the limit (2) is now exhausted by parent+child combined; a third
		// distinct, genuinely registered library must be refused from the
		// child's perspective too.
		got, err = child.LoadLibraryByHash(libs[2].Hash())
		if err != nil {
			t.Fatalf("child load third library: %v", err)
		}
		if got != nil {
			t.Fatalf("child load third library = %v, want nil (limit exhausted by parent)", got)
		}
		return 9, nil
	})

	exitCode, err := parent.RunChild(child)
	if err != nil {
		t.Fatalf("run child: %v", err)
	}
	if exitCode != 9 {
		t.Fatalf("child exit code = %d, want 9", exitCode)
	}

	// the counter is shared: the parent must see the child's consumption of
	// the second slot, and must itself now refuse the third library too.
	if n := len(parent.loadedLibraries); n != 2 {
		t.Fatalf("parent loadedLibraries after child run = %d, want 2 (shared with child)", n)
	}
	got, err := parent.LoadLibraryByHash(libs[2].Hash())
	if err != nil {
		t.Fatalf("parent load third library after child run: %v", err)
	}
	if got != nil {
		t.Fatalf("parent load third library after child run = %v, want nil (limit shared)", got)
	}

	// re-requesting the hash the child already loaded must still be free
	// from the parent side too, since it shares the same map.
	got, err = parent.LoadLibraryByHash(libs[1].Hash())
	if err != nil {
		t.Fatalf("parent re-load of child-loaded library: %v", err)
	}
	if got == nil || got.HashKey() != libs[1].HashKey() {
		t.Fatalf("parent re-load of child-loaded library = %v, want %x", got, libs[1].Hash())
	}
}

// The reverse direction of sharing: a limit consumed only via the parent
// before the child ever runs must still be visible to a freshly-run child
// even when the parent had never touched loadedLibraries beforehand (map
// lazily created inside prepareChildForRun itself).
func TestLibraryLoadLimitSharedLazyMapCreatedForChild(t *testing.T) {
	libs := distinctLibs(2)
	root := makeLibraryRootMulti(t, libs...)

	parent := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(1_000_000), nil, tuple.Tuple{}, NewStack())
	parent.InitForExecution()
	parent.SetLibraries(root)
	parent.SetMaxLibraryLoads(1)
	// note: parent never calls LoadLibraryByHash itself, so
	// parent.loadedLibraries is still nil going into RunChild.

	child := NewExecutionState(0, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
	child.CurrentCode = cell.BeginCell().MustStoreUInt(0xEF, 8).EndCell().MustBeginParse()

	parent.SetChildRunner(func(child *State) (int64, error) {
		if got, err := child.LoadLibraryByHash(libs[0].Hash()); err != nil || got == nil {
			t.Fatalf("child load first library = (%v, %v)", got, err)
		}
		if got, err := child.LoadLibraryByHash(libs[1].Hash()); err != nil || got != nil {
			t.Fatalf("child load second library = (%v, %v), want (nil, nil)", got, err)
		}
		return 0, nil
	})

	if _, err := parent.RunChild(child); err != nil {
		t.Fatalf("run child: %v", err)
	}

	if parent.loadedLibraries == nil || len(parent.loadedLibraries) != 1 {
		t.Fatalf("parent loadedLibraries = %v, want a shared map with 1 entry", parent.loadedLibraries)
	}
	if got, err := parent.LoadLibraryByHash(libs[1].Hash()); err != nil || got != nil {
		t.Fatalf("parent load second library after child run = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestMaxDataDepthDefaultsTo512(t *testing.T) {
	st := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	if st.maxDataDepth != 512 {
		t.Fatalf("default maxDataDepth = %d, want 512", st.maxDataDepth)
	}
	if MaxDataDepth != 512 {
		t.Fatalf("MaxDataDepth constant = %d, want 512", MaxDataDepth)
	}
}

// SetMaxDataDepth must actually change the limit enforced by
// TryCommitCurrent/ForceCommitCurrent, using a small configured depth so the
// test stays cheap instead of building a 512-deep cell.
func TestSetMaxDataDepthChangesCommitLimit(t *testing.T) {
	const depth = 3

	tooDeep := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), buildDeepCell(depth+1), tuple.Tuple{}, NewStack())
	tooDeep.SetMaxDataDepth(depth)
	tooDeep.Reg.D[1] = cell.BeginCell().EndCell()
	if tooDeep.maxDataDepth != depth {
		t.Fatalf("maxDataDepth = %d, want %d", tooDeep.maxDataDepth, depth)
	}
	if tooDeep.TryCommitCurrent() {
		t.Fatal("expected commit exceeding configured max data depth to fail")
	}
	if err := tooDeep.ForceCommitCurrent(); err == nil {
		t.Fatal("expected force commit exceeding configured max data depth to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeCellOverflow)
	}

	atLimit := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), buildDeepCell(depth), tuple.Tuple{}, NewStack())
	atLimit.SetMaxDataDepth(depth)
	atLimit.Reg.D[1] = cell.BeginCell().EndCell()
	if !atLimit.TryCommitCurrent() {
		t.Fatal("expected commit exactly at the configured max data depth to succeed")
	}

	// same depth cell, but with the (much larger) default limit still in
	// effect, must also succeed - proving the low limit above really came
	// from SetMaxDataDepth and not some unrelated depth restriction.
	unconfigured := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), buildDeepCell(depth+1), tuple.Tuple{}, NewStack())
	unconfigured.Reg.D[1] = cell.BeginCell().EndCell()
	if !unconfigured.TryCommitCurrent() {
		t.Fatal("expected commit within the default max data depth to succeed when SetMaxDataDepth was never called")
	}
}
