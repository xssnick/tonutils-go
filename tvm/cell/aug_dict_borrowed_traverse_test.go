package cell

import (
	"errors"
	"reflect"
	"testing"
)

type borrowedTraverseEvent struct {
	prefixBits uint
	prefix     uint64
	extra      uint64
	value      int
}

type borrowedTraversePlan struct {
	forkDirective int
	leafDirective int
	stopAt        int
}

func newBorrowedTraverseTestDict(tb testing.TB) *AugmentedDictionary {
	tb.Helper()

	dict, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		tb.Fatal(err)
	}
	for _, pair := range []struct {
		key   uint64
		value uint64
	}{
		{key: 0x10, value: 0xa1},
		{key: 0x11, value: 0xb2},
		{key: 0x20, value: 0xc3},
		{key: 0x80, value: 0xd4},
		{key: 0x81, value: 0xe5},
	} {
		key := BeginCell().MustStoreUInt(pair.key, 8).EndCell()
		value := BeginCell().MustStoreUInt(pair.value, 8).
			MustStoreRef(BeginCell().MustStoreUInt(pair.value^0xff, 8).EndCell()).
			EndCell()
		if err = dict.Set(key, value); err != nil {
			tb.Fatal(err)
		}
	}
	return dict
}

func borrowedTraverseSnapshot(tb testing.TB, key, extra, value *Slice) borrowedTraverseEvent {
	tb.Helper()

	keyView := *key
	event := borrowedTraverseEvent{
		prefixBits: keyView.BitsLeft(),
		value:      -1,
	}
	var err error
	if event.prefixBits != 0 {
		event.prefix, err = keyView.LoadUInt(event.prefixBits)
		if err != nil {
			tb.Fatal(err)
		}
	}

	extraView := *extra
	event.extra, err = extraView.LoadUInt(16)
	if err != nil {
		tb.Fatal(err)
	}
	if extraView.BitsLeft() != 0 || extraView.RefsNum() != 0 {
		tb.Fatalf("extra has %d trailing bits and %d refs", extraView.BitsLeft(), extraView.RefsNum())
	}

	if value != nil {
		valueView := *value
		loaded, err := valueView.LoadUInt(8)
		if err != nil {
			tb.Fatal(err)
		}
		event.value = int(loaded)
	}
	return event
}

func borrowedTraverseDirective(plan borrowedTraversePlan, event borrowedTraverseEvent) int {
	if event.value < 0 {
		return plan.forkDirective
	}
	if event.value == plan.stopAt {
		return 1
	}
	return plan.leafDirective
}

func collectLegacyTraverseEvents(tb testing.TB, dict *AugmentedDictionary, plan borrowedTraversePlan) ([]borrowedTraverseEvent, bool, error) {
	tb.Helper()

	events := make([]borrowedTraverseEvent, 0, 16)
	value, _, err := dict.TraverseExtra(func(key *Cell, extra, value *Slice) (int, error) {
		var keyView Slice
		if err := key.BeginParseInto(&keyView); err != nil {
			return 0, err
		}
		event := borrowedTraverseSnapshot(tb, &keyView, extra, value)
		events = append(events, event)
		return borrowedTraverseDirective(plan, event), nil
	})
	return events, value != nil, err
}

func collectBorrowedTraverseEvents(tb testing.TB, dict *AugmentedDictionary, plan borrowedTraversePlan) ([]borrowedTraverseEvent, bool, error) {
	tb.Helper()

	events := make([]borrowedTraverseEvent, 0, 16)
	found := false
	err := dict.TraverseExtraBorrowed(func(key, extra, value *Slice) (int, error) {
		event := borrowedTraverseSnapshot(tb, key, extra, value)
		events = append(events, event)
		directive := borrowedTraverseDirective(plan, event)
		if value != nil && directive > 0 {
			found = true
		}
		return directive, nil
	})
	return events, found, err
}

func TestAugmentedDictionaryTraverseExtraBorrowedMatchesLegacyDFS(t *testing.T) {
	dict := newBorrowedTraverseTestDict(t)

	tests := []struct {
		name string
		plan borrowedTraversePlan
	}{
		{name: "prune root", plan: borrowedTraversePlan{forkDirective: 0, stopAt: -1}},
		{name: "left only", plan: borrowedTraversePlan{forkDirective: 1, stopAt: -1}},
		{name: "right only", plan: borrowedTraversePlan{forkDirective: 2, stopAt: -1}},
		{name: "right before left", plan: borrowedTraversePlan{forkDirective: 5, stopAt: -1}},
		{name: "left before right", plan: borrowedTraversePlan{forkDirective: 6, stopAt: -1}},
		{name: "early leaf stop", plan: borrowedTraversePlan{forkDirective: 6, stopAt: 0xc3}},
		{name: "negative leaf continues", plan: borrowedTraversePlan{forkDirective: 6, leafDirective: -1, stopAt: -1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			legacyEvents, legacyFound, legacyErr := collectLegacyTraverseEvents(t, dict, test.plan)
			borrowedEvents, borrowedFound, borrowedErr := collectBorrowedTraverseEvents(t, dict, test.plan)
			if legacyErr != nil || borrowedErr != nil {
				t.Fatalf("legacy error=%v, borrowed error=%v", legacyErr, borrowedErr)
			}
			if legacyFound != borrowedFound {
				t.Fatalf("legacy found=%v, borrowed found=%v", legacyFound, borrowedFound)
			}
			if !reflect.DeepEqual(borrowedEvents, legacyEvents) {
				t.Fatalf("borrowed events=%+v, legacy events=%+v", borrowedEvents, legacyEvents)
			}
		})
	}
}

func TestAugmentedDictionaryTraverseExtraBorrowedOwnedKeysSurviveAdvance(t *testing.T) {
	dict := newBorrowedTraverseTestDict(t)
	var toCells, baseCells []*Cell

	err := dict.TraverseExtraBorrowed(func(key, _ *Slice, value *Slice) (int, error) {
		if value == nil {
			return 6, nil
		}

		owned, err := key.ToCell()
		if err != nil {
			return 0, err
		}
		toCells = append(toCells, owned)
		baseCells = append(baseCells, key.BaseCell())
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := []uint64{0x10, 0x11, 0x20, 0x80, 0x81}
	if len(toCells) != len(expected) || len(baseCells) != len(expected) {
		t.Fatalf("owned key counts: ToCell=%d BaseCell=%d", len(toCells), len(baseCells))
	}
	for i, expectedKey := range expected {
		if got := toCells[i].MustBeginParse().MustLoadUInt(8); got != expectedKey {
			t.Fatalf("ToCell key %d = %x, want %x", i, got, expectedKey)
		}
		if got := baseCells[i].MustBeginParse().MustLoadUInt(8); got != expectedKey {
			t.Fatalf("BaseCell key %d = %x, want %x", i, got, expectedKey)
		}
	}
}

func consumeBorrowedTraverseValue(value *Slice) error {
	if value == nil {
		return nil
	}
	if _, err := value.LoadUInt(8); err != nil {
		return err
	}
	if value.RefsNum() == 0 {
		return nil
	}
	var ref Slice
	if err := value.LoadRefInto(&ref); err != nil {
		return err
	}
	_, err := ref.LoadUInt(8)
	return err
}

func runLegacyBorrowedTraverseReadSet(t *testing.T, dict *AugmentedDictionary) *ReadSet {
	t.Helper()

	read := NewReadSet(dict.root)
	view := dict.CopyWithTrace(read.Trace())
	_, _, err := view.TraverseExtra(func(_ *Cell, _ *Slice, value *Slice) (int, error) {
		if value == nil {
			return 1, nil
		}
		if err := consumeBorrowedTraverseValue(value); err != nil {
			return 0, err
		}
		return 1, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return read
}

func runBorrowedTraverseReadSet(t *testing.T, dict *AugmentedDictionary) *ReadSet {
	t.Helper()

	read := NewReadSet(dict.root)
	view := dict.CopyWithTrace(read.Trace())
	if err := view.TraverseExtraBorrowed(func(_, _ *Slice, value *Slice) (int, error) {
		if value == nil {
			return 1, nil
		}
		if err := consumeBorrowedTraverseValue(value); err != nil {
			return 0, err
		}
		return 1, nil
	}); err != nil {
		t.Fatal(err)
	}
	return read
}

func TestAugmentedDictionaryTraverseExtraBorrowedPreservesTraceBoundary(t *testing.T) {
	dict := newBorrowedTraverseTestDict(t)
	legacyRead := runLegacyBorrowedTraverseReadSet(t, dict)
	borrowedRead := runBorrowedTraverseReadSet(t, dict)

	legacyHashes := make(map[Hash]struct{}, legacyRead.Size())
	for _, hash := range legacyRead.Hashes() {
		legacyHashes[hash] = struct{}{}
	}
	borrowedHashes := make(map[Hash]struct{}, borrowedRead.Size())
	for _, hash := range borrowedRead.Hashes() {
		borrowedHashes[hash] = struct{}{}
	}
	if !reflect.DeepEqual(borrowedHashes, legacyHashes) {
		t.Fatalf("borrowed read set has %d cells, legacy has %d", len(borrowedHashes), len(legacyHashes))
	}

	legacyProof, err := legacyRead.Proof()
	if err != nil {
		t.Fatal(err)
	}
	borrowedProof, err := borrowedRead.Proof()
	if err != nil {
		t.Fatal(err)
	}
	if borrowedProof.HashKey() != legacyProof.HashKey() {
		t.Fatalf("borrowed proof hash=%x, legacy proof hash=%x", borrowedProof.HashKey(), legacyProof.HashKey())
	}
}

func TestAugmentedDictionaryTraverseExtraBorrowedLazyAndErrors(t *testing.T) {
	dict := newBorrowedTraverseTestDict(t)

	t.Run("lazy parity", func(t *testing.T) {
		legacyLoader := testLazyLoaderForCellTree(dict.root)
		legacy := &AugmentedDictionary{
			keySz: dict.keySz,
			root:  cellWithLazyRefsFromCell(dict.root, legacyLoader.LoadCell),
			aug:   dict.aug,
		}
		borrowedLoader := testLazyLoaderForCellTree(dict.root)
		borrowed := &AugmentedDictionary{
			keySz: dict.keySz,
			root:  cellWithLazyRefsFromCell(dict.root, borrowedLoader.LoadCell),
			aug:   dict.aug,
		}
		plan := borrowedTraversePlan{forkDirective: 6, stopAt: -1}
		legacyEvents, _, legacyErr := collectLegacyTraverseEvents(t, legacy, plan)
		borrowedEvents, _, borrowedErr := collectBorrowedTraverseEvents(t, borrowed, plan)
		if legacyErr != nil || borrowedErr != nil {
			t.Fatalf("legacy error=%v, borrowed error=%v", legacyErr, borrowedErr)
		}
		if !reflect.DeepEqual(borrowedEvents, legacyEvents) {
			t.Fatalf("borrowed events=%+v, legacy events=%+v", borrowedEvents, legacyEvents)
		}
		if borrowedLoader.calls == 0 || borrowedLoader.calls != legacyLoader.calls {
			t.Fatalf("borrowed lazy loads=%d, legacy lazy loads=%d", borrowedLoader.calls, legacyLoader.calls)
		}
	})

	t.Run("callback error", func(t *testing.T) {
		callbackErr := errors.New("borrowed callback failed")
		calls := 0
		err := dict.TraverseExtraBorrowed(func(_, _ *Slice, _ *Slice) (int, error) {
			calls++
			return 0, callbackErr
		})
		if !errors.Is(err, callbackErr) || calls != 1 {
			t.Fatalf("error=%v calls=%d", err, calls)
		}
	})

	t.Run("pending load error", func(t *testing.T) {
		pendingErr := errors.New("pending borrowed load error")
		trace, loads := pendingErrorAfterLoads(1, pendingErr)
		view := dict.CopyWithTrace(trace)
		calls := 0
		err := view.TraverseExtraBorrowed(func(_, _ *Slice, _ *Slice) (int, error) {
			calls++
			return 0, nil
		})
		if !errors.Is(err, pendingErr) || calls != 0 || loads() != 1 {
			t.Fatalf("error=%v calls=%d loads=%d", err, calls, loads())
		}
	})

	t.Run("invalid fork directives", func(t *testing.T) {
		for _, directive := range []int{-1, 3, 7, 9} {
			err := dict.TraverseExtraBorrowed(func(_, _ *Slice, value *Slice) (int, error) {
				if value == nil {
					return directive, nil
				}
				return 0, nil
			})
			if err == nil {
				t.Fatalf("directive %d did not fail", directive)
			}
		}
	})

	t.Run("special node", func(t *testing.T) {
		library, err := BeginCell().
			MustStoreUInt(uint64(LibraryCellType), 8).
			MustStoreSlice(make([]byte, 32), 256).
			EndCellSpecial(true)
		if err != nil {
			t.Fatal(err)
		}
		bad := &AugmentedDictionary{keySz: 8, root: library, aug: testMetricAugmentation{}}
		if err = bad.TraverseExtraBorrowed(func(_, _ *Slice, _ *Slice) (int, error) { return 0, nil }); err == nil {
			t.Fatal("special node did not fail")
		}
	})

	t.Run("empty inputs", func(t *testing.T) {
		if err := (*AugmentedDictionary)(nil).TraverseExtraBorrowed(nil); err != nil {
			t.Fatal(err)
		}
		if err := dict.TraverseExtraBorrowed(nil); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAugmentedDictionaryTraverseExtraBorrowedAllocationsAreConstant(t *testing.T) {
	dict := borrowedIntoBenchmarkAugDict(t, 2048)
	allocs := testing.AllocsPerRun(20, func() {
		if err := dict.TraverseExtraBorrowed(borrowedTraverseBenchmarkCallback); err != nil {
			panic(err)
		}
	})
	if allocs > 2 {
		t.Fatalf("TraverseExtraBorrowed allocations = %.0f, want at most 2 per walk", allocs)
	}
}

func borrowedTraverseBenchmarkCallback(_ *Slice, _ *Slice, value *Slice) (int, error) {
	if value == nil {
		return 6, nil
	}
	return 0, nil
}

func legacyTraverseBenchmarkCallback(_ *Cell, _ *Slice, value *Slice) (int, error) {
	if value == nil {
		return 6, nil
	}
	return 0, nil
}

func BenchmarkAugmentedDictionaryTraverseExtraBorrowed(b *testing.B) {
	dict := borrowedIntoBenchmarkAugDict(b, 2048)

	b.Run("legacy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := dict.TraverseExtra(legacyTraverseBenchmarkCallback); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("borrowed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := dict.TraverseExtraBorrowed(borrowedTraverseBenchmarkCallback); err != nil {
				b.Fatal(err)
			}
		}
	})
}
