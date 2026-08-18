package cell

import (
	"fmt"
	"math/big"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func mustPlainDiffDict(t *testing.T, entries map[uint64]uint64) *Dictionary {
	t.Helper()
	dict := NewDict(16)
	for key, value := range entries {
		if err := dict.SetIntKey(new(big.Int).SetUint64(key), BeginCell().MustStoreUInt(value, 32).EndCell()); err != nil {
			t.Fatal(err)
		}
	}
	return dict
}

func plainDiffValue(value *Slice) string {
	if value == nil {
		return "-"
	}
	copied := *value
	v, err := copied.LoadUInt(32)
	if err != nil {
		return "err"
	}
	return fmt.Sprintf("%08x", v)
}

func collectPlainDiff(t *testing.T, old, next *Dictionary) []string {
	t.Helper()
	var got []string
	err := old.ScanDiff(next, func(key *Cell, oldValue, newValue *Slice) error {
		keyValue := key.MustBeginParse().MustLoadUInt(16)
		got = append(got, fmt.Sprintf("%04x:%s:%s", keyValue, plainDiffValue(oldValue), plainDiffValue(newValue)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestDictionaryScanDiffReportsChangedLeaves(t *testing.T) {
	old := mustPlainDiffDict(t, map[uint64]uint64{
		0x0000: 0xa0,
		0x4000: 0xa1,
		0x8000: 0xa2,
		0xc000: 0xa3,
	})
	next := mustPlainDiffDict(t, map[uint64]uint64{
		0x1000: 0xb0,
		0x4000: 0xa1,
		0x8000: 0xb2,
		0xe000: 0xb3,
	})

	got := collectPlainDiff(t, old, next)
	want := []string{
		"0000:000000a0:-",
		"1000:-:000000b0",
		"8000:000000a2:000000b2",
		"c000:000000a3:-",
		"e000:-:000000b3",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("scan diff produced %v, want %v", got, want)
	}
}

func TestDictionaryScanDiffSkipsEqualDictionaries(t *testing.T) {
	entries := map[uint64]uint64{0x0001: 1, 0x8000: 2, 0xffff: 3}
	old := mustPlainDiffDict(t, entries)
	next := mustPlainDiffDict(t, entries)

	if got := collectPlainDiff(t, old, next); len(got) != 0 {
		t.Fatalf("equal dictionaries produced %v", got)
	}
}

// TestDictionaryScanDiffSkipsEqualRootByHash proves the skip by counting cell
// loads rather than callbacks: a walk that descended and compared would still
// report no changes, so only the load count distinguishes the two.
func TestDictionaryScanDiffSkipsEqualRootByHash(t *testing.T) {
	dict := mustPlainDiffDict(t, map[uint64]uint64{
		0x0000: 0xa0,
		0x4000: 0xb0,
		0x8000: 0xc0,
		0xc000: 0xd0,
	})

	loads := 0
	trace := NewTrace(TraceHooks{OnLoad: func(*Cell) { loads++ }})
	old := dict.Copy().SetTrace(trace)
	next := dict.Copy().SetTrace(trace)
	if err := old.ScanDiff(next, func(*Cell, *Slice, *Slice) error {
		t.Fatal("callback called for equal dictionaries")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if loads != 0 {
		t.Fatalf("equal-root cell loads = %d, want 0", loads)
	}
}

// TestDictionaryScanDiffSkipsLazyBoundaryByHash is the property the collator
// depends on: dictionaries read from CellDB carry lazy boundaries, and an
// untouched subtree behind one must be skipped without ever being fetched. A
// lazy placeholder carries the represented subtree's hash, so the hash shortcut
// fires before the loader is ever consulted.
func TestDictionaryScanDiffSkipsLazyBoundaryByHash(t *testing.T) {
	entries := map[uint64]uint64{}
	for i := uint64(0); i < 32; i++ {
		entries[0x8000|i] = i
	}
	entries[0x0001] = 1
	old := mustPlainDiffDict(t, entries)

	entries[0x0001] = 2
	next := mustPlainDiffDict(t, entries)

	loader := &testLazyLoader{cells: map[Hash]*Cell{}}
	for _, ref := range old.AsCell().rawRefs() {
		loader.cells[ref.HashKey()] = ref
	}

	lazyRoot := cellWithLazyRefsFromCell(old.AsCell(), loader.LoadCell)
	lazyDict := lazyRoot.AsDict(16)

	visited := 0
	if err := lazyDict.ScanDiff(next, func(key *Cell, oldValue, newValue *Slice) error {
		visited++
		if got := key.MustBeginParse().MustLoadUInt(16); got != 0x0001 {
			t.Fatalf("visited untouched key %04x", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if visited != 1 {
		t.Fatalf("visited %d leaves, want exactly the changed one", visited)
	}
	if loader.calls > 1 {
		t.Fatalf("lazy loader called %d times; the untouched subtree was fetched", loader.calls)
	}
}

// TestDictionaryScanDiffSkipsSharedSubtreeByHash pins the property the whole
// optimization rests on: an untouched subtree is never descended into.
func TestDictionaryScanDiffSkipsSharedSubtreeByHash(t *testing.T) {
	shared := map[uint64]uint64{}
	for i := uint64(0); i < 64; i++ {
		shared[0x8000|i] = i
	}
	oldEntries := map[uint64]uint64{0x0001: 1}
	nextEntries := map[uint64]uint64{0x0001: 2}
	for key, value := range shared {
		oldEntries[key] = value
		nextEntries[key] = value
	}

	old := mustPlainDiffDict(t, oldEntries)
	next := mustPlainDiffDict(t, nextEntries)

	visited := 0
	err := old.ScanDiff(next, func(key *Cell, oldValue, newValue *Slice) error {
		visited++
		if got := key.MustBeginParse().MustLoadUInt(16); got != 0x0001 {
			t.Fatalf("visited untouched key %04x", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if visited != 1 {
		t.Fatalf("visited %d leaves, want exactly the changed one", visited)
	}
}

func TestDictionaryScanDiffEmptySides(t *testing.T) {
	populated := mustPlainDiffDict(t, map[uint64]uint64{0x0000: 1, 0xffff: 2})
	empty := NewDict(16)

	if got := collectPlainDiff(t, empty, empty); len(got) != 0 {
		t.Fatalf("two empty dictionaries produced %v", got)
	}

	got := collectPlainDiff(t, empty, populated)
	want := []string{"0000:-:00000001", "ffff:-:00000002"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("insert-only diff produced %v, want %v", got, want)
	}

	got = collectPlainDiff(t, populated, empty)
	want = []string{"0000:00000001:-", "ffff:00000002:-"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("delete-only diff produced %v, want %v", got, want)
	}
}

func TestDictionaryScanDiffNilReceivers(t *testing.T) {
	var absent *Dictionary
	populated := mustPlainDiffDict(t, map[uint64]uint64{0x0000: 1})

	// A nil dictionary has key size zero, so it can only be compared to another
	// nil one; anything else is a key-size mismatch rather than a silent empty
	// diff.
	if err := absent.ScanDiff(populated, func(*Cell, *Slice, *Slice) error { return nil }); err == nil {
		t.Fatal("comparing a nil dictionary to a 16-bit one must fail")
	}
	if err := populated.ScanDiff(absent, func(*Cell, *Slice, *Slice) error { return nil }); err == nil {
		t.Fatal("comparing a 16-bit dictionary to a nil one must fail")
	}
}

func TestDictionaryScanDiffRejectsKeySizeMismatch(t *testing.T) {
	left := NewDict(16)
	right := NewDict(32)
	if err := left.ScanDiff(right, func(*Cell, *Slice, *Slice) error { return nil }); err == nil {
		t.Fatal("expected key size mismatch to be rejected")
	}
}

func TestDictionaryScanDiffPropagatesCallbackError(t *testing.T) {
	old := mustPlainDiffDict(t, map[uint64]uint64{0x0000: 1})
	next := mustPlainDiffDict(t, map[uint64]uint64{0x0000: 2})

	want := fmt.Errorf("stop here")
	err := old.ScanDiff(next, func(*Cell, *Slice, *Slice) error { return want })
	if err == nil || !strings.Contains(err.Error(), "stop here") {
		t.Fatalf("callback error was not propagated: %v", err)
	}
}

// TestDictionaryScanDiffMatchesBruteForce is the real gate: over randomized
// dictionaries the diff must report exactly the symmetric difference that a
// full materialization of both sides produces, in ascending key order.
func TestDictionaryScanDiffMatchesBruteForce(t *testing.T) {
	rnd := rand.New(rand.NewSource(20260814))

	for round := range 400 {
		size := rnd.Intn(24)
		oldEntries := map[uint64]uint64{}
		for range size {
			oldEntries[uint64(rnd.Intn(1<<16))] = uint64(rnd.Intn(1 << 20))
		}
		nextEntries := map[uint64]uint64{}
		for key, value := range oldEntries {
			switch rnd.Intn(4) {
			case 0: // deleted
			case 1: // modified
				nextEntries[key] = value + 1 + uint64(rnd.Intn(8))
			default:
				nextEntries[key] = value
			}
		}
		for range rnd.Intn(8) {
			nextEntries[uint64(rnd.Intn(1<<16))] = uint64(rnd.Intn(1 << 20))
		}

		old := mustPlainDiffDict(t, oldEntries)
		next := mustPlainDiffDict(t, nextEntries)

		var want []string
		keys := map[uint64]struct{}{}
		for key := range oldEntries {
			keys[key] = struct{}{}
		}
		for key := range nextEntries {
			keys[key] = struct{}{}
		}
		ordered := make([]uint64, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
		for _, key := range ordered {
			oldValue, hadOld := oldEntries[key]
			newValue, hasNew := nextEntries[key]
			if hadOld && hasNew && oldValue == newValue {
				continue
			}
			render := func(value uint64, present bool) string {
				if !present {
					return "-"
				}
				return fmt.Sprintf("%08x", value)
			}
			want = append(want, fmt.Sprintf("%04x:%s:%s", key,
				render(oldValue, hadOld), render(newValue, hasNew)))
		}

		got := collectPlainDiff(t, old, next)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("round %d: diff of %v -> %v produced\n got %v\nwant %v",
				round, oldEntries, nextEntries, got, want)
		}
	}
}

// TestDictionaryScanDiffDeepSuffixes exercises the label-alignment paths where
// one side's edge label is longer than the other's, which is where the skip
// bookkeeping earns its keep.
func TestDictionaryScanDiffDeepSuffixes(t *testing.T) {
	old := mustPlainDiffDict(t, map[uint64]uint64{
		0xff00: 1,
		0xff01: 2,
	})
	next := mustPlainDiffDict(t, map[uint64]uint64{
		0xff00: 1,
		0xff01: 2,
		0xff02: 3,
		0xff80: 4,
	})

	got := collectPlainDiff(t, old, next)
	want := []string{"ff02:-:00000003", "ff80:-:00000004"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("deep suffix diff produced %v, want %v", got, want)
	}

	got = collectPlainDiff(t, next, old)
	want = []string{"ff02:00000003:-", "ff80:00000004:-"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("reverse deep suffix diff produced %v, want %v", got, want)
	}
}

// TestDictionaryScanDiffSingleEntryRoots covers the degenerate trie where the
// root itself is the leaf and the two roots carry different full-length labels.
func TestDictionaryScanDiffSingleEntryRoots(t *testing.T) {
	old := mustPlainDiffDict(t, map[uint64]uint64{0x1234: 7})
	next := mustPlainDiffDict(t, map[uint64]uint64{0x5678: 8})

	got := collectPlainDiff(t, old, next)
	want := []string{"1234:00000007:-", "5678:-:00000008"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("disjoint single-entry diff produced %v, want %v", got, want)
	}

	same := mustPlainDiffDict(t, map[uint64]uint64{0x1234: 9})
	got = collectPlainDiff(t, old, same)
	want = []string{"1234:00000007:00000009"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("same-key single-entry diff produced %v, want %v", got, want)
	}
}

// TestDictionaryScanDiffRejectsPrunedAgainstReal pins the boundary policy: a
// pruned branch facing a real subtree cannot be compared, so the walk must fail
// rather than silently report the subtree as unchanged.
func TestDictionaryScanDiffRejectsPrunedAgainstReal(t *testing.T) {
	old := mustPlainDiffDict(t, map[uint64]uint64{0x0000: 1, 0x8000: 2})
	next := mustPlainDiffDict(t, map[uint64]uint64{0x0000: 1, 0x8000: 3})

	pruned, err := CreatePrunedBranch(old.AsCell(), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pruned.GetType() != PrunedCellType {
		t.Fatalf("expected a pruned branch, got type %d", pruned.GetType())
	}
	prunedDict := pruned.AsDict(16)

	if err = prunedDict.ScanDiff(next, func(*Cell, *Slice, *Slice) error { return nil }); err == nil {
		t.Fatal("expected a pruned boundary facing a real subtree to fail")
	}
	if err = next.ScanDiff(prunedDict, func(*Cell, *Slice, *Slice) error { return nil }); err == nil {
		t.Fatal("expected a real subtree facing a pruned boundary to fail")
	}
	// The same pruned branch on both sides is still skipped by hash, so an
	// unchanged pruned subtree costs nothing and raises nothing.
	if got := collectPlainDiff(t, prunedDict, prunedDict); len(got) != 0 {
		t.Fatalf("identical pruned roots produced %v", got)
	}
}
