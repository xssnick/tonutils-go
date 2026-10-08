package vm

import (
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

var benchmarkMissingLibraryResult *cell.Hash

func assertMissingLibrary(t *testing.T, state *State, want *cell.Hash) {
	t.Helper()

	got := state.MissingLibrary()
	if want == nil {
		if got != nil {
			t.Fatalf("missing library = %x, want nil", *got)
		}
		return
	}
	if got == nil || *got != *want {
		t.Fatalf("missing library = %v, want %x", got, *want)
	}
}

func TestMissingLibraryTracksLastActualMiss(t *testing.T) {
	real := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	state := newLibraryLimitState(t, real)
	state.SetMaxLibraryLoads(3)

	missingA := cell.Hash{0x11}
	missingB := cell.Hash{0x22}
	refused := cell.Hash{0x33}

	assertMissingLibrary(t, state, nil)
	if got, err := state.LoadLibraryByHash(missingA[:]); err != nil || got != nil {
		t.Fatalf("load missing A = (%v, %v), want (nil, nil)", got, err)
	}
	assertMissingLibrary(t, state, &missingA)
	firstResult := state.MissingLibrary()

	if got, err := state.LoadLibraryByHash(real.Hash()); err != nil || got == nil || got.HashKey() != real.HashKey() {
		t.Fatalf("load real library = (%v, %v), want (%x, nil)", got, err, real.Hash())
	}
	assertMissingLibrary(t, state, &missingA)

	if got, err := state.LoadLibraryByHash(missingB[:]); err != nil || got != nil {
		t.Fatalf("load missing B = (%v, %v), want (nil, nil)", got, err)
	}
	if firstResult == nil || *firstResult != missingA {
		t.Fatalf("previous missing-library result changed to %v after the next miss", firstResult)
	}
	assertMissingLibrary(t, state, &missingB)

	// Three distinct attempts have exhausted the limit. C++ returns before
	// searching any collection here, so this refusal must not look like a miss.
	if got, err := state.LoadLibraryByHash(refused[:]); err != nil || got != nil {
		t.Fatalf("load refused library = (%v, %v), want (nil, nil)", got, err)
	}
	assertMissingLibrary(t, state, &missingB)

	// Repeating a seen successful hash still performs its lookup at the limit,
	// but success does not clear the last missing hash.
	if got, err := state.LoadLibraryByHash(real.Hash()); err != nil || got == nil || got.HashKey() != real.HashKey() {
		t.Fatalf("repeat real library = (%v, %v), want (%x, nil)", got, err, real.Hash())
	}
	assertMissingLibrary(t, state, &missingB)
}

func TestSuspendedLibraryLookupUsesIsolatedResultState(t *testing.T) {
	real := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	state := newLibraryLimitState(t, real)
	missingA := cell.Hash{0x41}
	missingB := cell.Hash{0x42}

	if got, err := state.LoadLibraryByHash(missingA[:]); err != nil || got != nil {
		t.Fatalf("load missing A = (%v, %v), want (nil, nil)", got, err)
	}
	assertMissingLibrary(t, state, &missingA)

	restore := state.SuspendLibraryLoadAccounting()
	if got, err := state.LoadLibraryByHash(real.Hash()); err != nil || got == nil || got.HashKey() != real.HashKey() {
		t.Fatalf("sandboxed real library = (%v, %v), want (%x, nil)", got, err, real.Hash())
	}
	if got, err := state.LoadLibraryByHash(missingB[:]); err != nil || got != nil {
		t.Fatalf("sandboxed missing B = (%v, %v), want (nil, nil)", got, err)
	}
	restore()

	assertMissingLibrary(t, state, &missingA)
	if state.libraryCache != nil {
		t.Fatalf("sandboxed startup lookup populated execution cache with %d entries", len(state.libraryCache))
	}
}

func TestDetachedCellManagerTraceCannotCallReleasedStateResolver(t *testing.T) {
	state := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
	state.Cells.Init(state)
	trace := state.Cells.Trace()
	state.Cells.FinishExecution()

	library, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(make([]byte, 32), 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatal(err)
	}
	key := cell.BeginCell().MustStoreUInt(0, 1).EndCell()
	if _, err = library.AsDictWithTrace(1, trace).LoadValue(key); !errors.Is(err, cell.ErrDictHasSpecialCells) {
		t.Fatalf("detached trace special-node error = %v, want ErrDictHasSpecialCells", err)
	}
}

func BenchmarkMissingLibraryResult(b *testing.B) {
	absent := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
	present := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
	present.libraryLoads = &libraryLoadState{missingLibrary: cell.Hash{0xAA}, hasMissing: true}

	b.Run("absent", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkMissingLibraryResult = absent.MissingLibrary()
		}
	})
	b.Run("present", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkMissingLibraryResult = present.MissingLibrary()
		}
	})
	b.Run("first_miss", func(b *testing.B) {
		var hash cell.Hash
		hash[0] = 0xBB
		b.ReportAllocs()
		for b.Loop() {
			state := State{}
			if _, err := state.LoadLibraryByHash(hash[:]); err != nil {
				b.Fatal(err)
			}
			benchmarkMissingLibraryResult = state.MissingLibrary()
		}
	})
	b.Run("sandboxed_miss", func(b *testing.B) {
		var hash cell.Hash
		hash[0] = 0xBB
		b.ReportAllocs()
		for b.Loop() {
			state := State{libraryLookupSandboxed: true}
			if _, err := state.LoadLibraryByHash(hash[:]); err != nil {
				b.Fatal(err)
			}
			benchmarkMissingLibraryResult = state.MissingLibrary()
		}
	})
}
