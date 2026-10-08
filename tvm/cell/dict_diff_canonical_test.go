package cell

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
)

type canonicalDiffCase struct {
	name       string
	keyBits    uint
	old, new   *Cell
	want       []string
	wantReject bool
}

func TestDictionaryScanDiffCanonicalNewLabels(t *testing.T) {
	for _, tc := range canonicalDiffCases(t, false) {
		for _, api := range []string{"cell", "borrowed", "raw"} {
			t.Run(tc.name+"/"+api, func(t *testing.T) {
				old, next := tc.old.AsDict(tc.keyBits), tc.new.AsDict(tc.keyBits)
				for _, check := range []bool{false, true} {
					var got []string
					emit := func(key uint64, oldValue, newValue *Slice) error {
						got = append(got, canonicalDiffEntry(key, oldValue, newValue, false))
						return nil
					}
					var options []DictDiffOption
					if check {
						options = []DictDiffOption{DictDiffCheckNewCanonicalLabels}
					}
					var err error
					switch api {
					case "cell":
						err = old.ScanDiff(next, func(key *Cell, oldValue, newValue *Slice) error {
							return emit(key.MustBeginParse().MustLoadUInt(tc.keyBits), oldValue, newValue)
						}, options...)
					case "borrowed":
						err = old.ScanDiffBorrowed(next, func(view DictDiffView) error {
							return emit(view.Key.MustLoadUInt(tc.keyBits), canonicalDiffValue(view.HasOld, view.OldValue), canonicalDiffValue(view.HasNew, view.NewValue))
						}, options...)
					case "raw":
						err = old.ScanDiffRaw(next, func(view DictDiffRawView) error {
							key := BeginCell().MustStoreSlice(view.Key, view.KeyBits).ToSlice().MustLoadUInt(tc.keyBits)
							return emit(key, canonicalDiffValue(view.HasOld, view.OldValue), canonicalDiffValue(view.HasNew, view.NewValue))
						}, options...)
					}
					assertCanonicalDiffResult(t, tc, check, got, err)
				}
			})
		}
	}
}

func TestAugmentedDictionaryScanDiffCanonicalNewLabels(t *testing.T) {
	for _, tc := range canonicalDiffCases(t, true) {
		for _, api := range []string{"cell", "borrowed", "raw", "parallel_cell", "parallel_borrowed", "parallel_raw", "parallel_cell_one_worker", "parallel_borrowed_one_worker", "parallel_raw_one_worker"} {
			t.Run(tc.name+"/"+api, func(t *testing.T) {
				old, next := tc.old.AsAugDict(tc.keyBits, testMetricAugmentation{}), tc.new.AsAugDict(tc.keyBits, testMetricAugmentation{})
				for _, check := range []bool{false, true} {
					var got []string
					var mu sync.Mutex
					emit := func(key uint64, oldValue, newValue *Slice) error {
						entry := canonicalDiffEntry(key, oldValue, newValue, true)
						mu.Lock()
						got = append(got, entry)
						mu.Unlock()
						return nil
					}
					cellFn := func(key *Cell, oldValue, newValue *Slice) error {
						return emit(key.MustBeginParse().MustLoadUInt(tc.keyBits), oldValue, newValue)
					}
					borrowedFn := func(view AugDictDiffView) error {
						return emit(view.Key.MustLoadUInt(tc.keyBits), canonicalDiffValue(view.HasOld, view.OldValueExtra), canonicalDiffValue(view.HasNew, view.NewValueExtra))
					}
					rawFn := func(view AugDictDiffRawView) error {
						key := BeginCell().MustStoreSlice(view.Key, view.KeyBits).ToSlice().MustLoadUInt(tc.keyBits)
						return emit(key, canonicalDiffValue(view.HasOld, view.OldValueExtra), canonicalDiffValue(view.HasNew, view.NewValueExtra))
					}
					var options []DictDiffOption
					if check {
						options = []DictDiffOption{DictDiffCheckNewCanonicalLabels}
					}
					var err error
					switch api {
					case "cell":
						err = old.ScanDiff(next, true, cellFn, options...)
					case "borrowed":
						err = old.ScanDiffBorrowed(next, true, borrowedFn, options...)
					case "raw":
						err = old.ScanDiffRaw(next, true, rawFn, options...)
					case "parallel_cell":
						err = old.ScanDiffParallel(next, true, cellFn, 4, options...)
					case "parallel_borrowed":
						err = old.ScanDiffParallelBorrowed(next, true, borrowedFn, 4, options...)
					case "parallel_raw":
						err = old.ScanDiffParallelRaw(next, true, rawFn, 4, options...)
					case "parallel_cell_one_worker":
						err = old.ScanDiffParallel(next, true, cellFn, 1, options...)
					case "parallel_borrowed_one_worker":
						err = old.ScanDiffParallelBorrowed(next, true, borrowedFn, 1, options...)
					case "parallel_raw_one_worker":
						err = old.ScanDiffParallelRaw(next, true, rawFn, 1, options...)
					}
					assertCanonicalDiffResult(t, tc, check, got, err)
				}
			})
		}
	}
}

func TestAugmentedDictionaryCanonicalDiffWithoutAugmentationChecks(t *testing.T) {
	aug := ReadOnlyAugmentation{SkipExtraFn: func(value *Slice) error { return value.SkipBits(16) }}
	empty, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	next := rawShortDictLabel(0xaa, 8).MustStoreUInt(8, 16).MustStoreUInt(1, 8).EndCell().AsAugDict(8, aug)
	callback := func(*Cell, *Slice, *Slice) error { return nil }
	if err = empty.ScanDiff(next, false, callback); err != nil {
		t.Fatalf("default diff rejected a noncanonical new label: %v", err)
	}
	if err = empty.ScanDiff(next, false, callback, DictDiffCheckNewCanonicalLabels); !errors.Is(err, ErrNonCanonicalDictLabel) {
		t.Fatalf("canonical check without augmentation validation = %v", err)
	}
}

func canonicalDiffCases(t *testing.T, augmented bool) []canonicalDiffCase {
	t.Helper()
	leaf := func(label *Builder, value uint64) *Cell {
		if augmented {
			label.MustStoreUInt(8, 16)
		}
		return label.MustStoreUInt(value, 8).EndCell()
	}
	fork := func(label *Builder, leafCount uint64, left, right *Cell) *Cell {
		label.MustStoreRef(left).MustStoreRef(right)
		if augmented {
			label.MustStoreUInt(leafCount*8, 16)
		}
		return label.EndCell()
	}
	clone := func(root *Cell) *Cell {
		copied, err := FromBOC(root.ToBOC())
		if err != nil {
			t.Fatal(err)
		}
		return copied
	}
	shortRoot := func(first, second uint64) *Cell {
		return fork(rawShortDictLabel(0b1010, 4), 2,
			leaf(rawSameDictLabel(0, 3, 3), first),
			leaf(rawSameDictLabel(0, 3, 3), second))
	}
	badRoot := fork(rawLongDictLabel(0b1010, 4, 8), 2,
		leaf(rawSameDictLabel(0, 3, 3), 1), leaf(rawSameDictLabel(0, 3, 3), 2))
	badLeaf := leaf(rawShortDictLabel(0xaa, 8), 1)
	newShorter := fork(rawShortDictLabel(0b1010, 4), 2,
		leaf(rawShortDictLabel(0, 3), 2), leaf(rawSameDictLabel(0, 3, 3), 3))
	oldShorter := fork(rawShortDictLabel(0b10, 2), 2,
		leaf(rawSameDictLabel(0, 5, 5), 1), leaf(rawSameDictLabel(0, 5, 5), 2))

	// This noncanonical subtree is unchanged while its sibling's value changes.
	shared := fork(rawLongDictLabel(0, 0, 7), 2,
		leaf(rawSameDictLabel(0, 6, 6), 1), leaf(rawSameDictLabel(0, 6, 6), 2))
	sharedOld := fork(rawShortDictLabel(0, 0), 3, shared, leaf(rawSameDictLabel(0, 7, 7), 3))
	sharedNew := fork(rawShortDictLabel(0, 0), 3, clone(shared), leaf(rawSameDictLabel(0, 7, 7), 4))

	// The shared fork is beyond the public parallel scan's depth-38 frontier.
	parallelLeft := fork(rawLongDictLabel(0, 0, 1), 2,
		leaf(rawShortDictLabel(0, 0), 1), leaf(rawShortDictLabel(0, 0), 2))
	parallelRight := fork(rawShortDictLabel(0, 0), 2,
		leaf(rawShortDictLabel(0, 0), 3), leaf(rawShortDictLabel(0, 0), 4))
	parallelOld := fork(rawSameDictLabel(0, 62, 64), 4, parallelLeft, parallelRight)
	parallelBad := fork(rawSameDictLabel(0, 62, 64), 4,
		fork(rawLongDictLabel(0, 0, 1), 2, leaf(rawShortDictLabel(0, 0), 9), leaf(rawShortDictLabel(0, 0), 2)), parallelRight)
	parallelShared := fork(rawSameDictLabel(0, 62, 64), 4, clone(parallelLeft),
		fork(rawShortDictLabel(0, 0), 2, leaf(rawShortDictLabel(0, 0), 9), leaf(rawShortDictLabel(0, 0), 4)))

	return []canonicalDiffCase{
		{name: "insert_noncanonical_leaf", keyBits: 8, new: badLeaf, want: []string{"aa:-:1"}, wantReject: true},
		{name: "noncanonical_encoding_equal_value", keyBits: 8, old: leaf(rawLongDictLabel(0xaa, 8, 8), 1), new: badLeaf, wantReject: true},
		{name: "changed_noncanonical_fork", keyBits: 8, old: shortRoot(3, 2), new: badRoot, want: []string{"a0:3:1"}, wantReject: true},
		{name: "new_shorter_bad_child", keyBits: 8, old: leaf(rawLongDictLabel(0xa0, 8, 8), 1), new: newShorter, want: []string{"a0:1:2", "a8:-:3"}, wantReject: true},
		{name: "new_longer_alignment", keyBits: 8, old: oldShorter, new: shortRoot(3, 4), want: []string{"80:1:-", "a0:2:3", "a8:-:4"}},
		{name: "old_noncanonical_allowed", keyBits: 8, old: badRoot, new: shortRoot(3, 2), want: []string{"a0:1:3"}},
		{name: "delete_noncanonical_allowed", keyBits: 8, old: badLeaf, want: []string{"aa:1:-"}},
		{name: "equal_noncanonical_root", keyBits: 8, old: badLeaf, new: clone(badLeaf)},
		{name: "shared_noncanonical_child", keyBits: 8, old: sharedOld, new: sharedNew, want: []string{"80:3:4"}},
		{name: "parallel_changed_noncanonical_child", keyBits: 64, old: parallelOld, new: parallelBad, want: []string{"0:1:9"}, wantReject: true},
		{name: "parallel_shared_noncanonical_child", keyBits: 64, old: parallelOld, new: parallelShared, want: []string{"2:3:9"}},
	}
}

func canonicalDiffValue(present bool, value Slice) *Slice {
	if !present {
		return nil
	}
	return &value
}

func canonicalDiffEntry(key uint64, oldValue, newValue *Slice, augmented bool) string {
	value := func(src *Slice) string {
		if src == nil {
			return "-"
		}
		loader := *src
		if augmented {
			loader.MustLoadUInt(16)
		}
		return fmt.Sprint(loader.MustLoadUInt(8))
	}
	return fmt.Sprintf("%x:%s:%s", key, value(oldValue), value(newValue))
}

func assertCanonicalDiffResult(t *testing.T, tc canonicalDiffCase, check bool, got []string, err error) {
	t.Helper()
	if check && tc.wantReject {
		if !errors.Is(err, ErrNonCanonicalDictLabel) {
			t.Fatalf("canonical check error = %v, want ErrNonCanonicalDictLabel", err)
		}
		if len(got) != 0 {
			t.Fatalf("callback ran before rejecting the noncanonical new node: %v", got)
		}
		return
	}
	if err != nil {
		t.Fatalf("check=%t: %v", check, err)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, tc.want) {
		t.Fatalf("check=%t: diff = %v, want %v", check, got, tc.want)
	}
}
