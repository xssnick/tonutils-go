package cell

import (
	"bytes"
	"errors"
	"testing"
)

type readOnlyTestAugmentation struct{}

func (readOnlyTestAugmentation) SkipExtra(loader *Slice) error {
	_, err := loader.LoadUInt(16)
	return err
}

func (readOnlyTestAugmentation) EmptyExtra() (*Cell, error) {
	return nil, ErrAugmentationSemanticsUnavailable
}

func (readOnlyTestAugmentation) LeafExtra(value *Slice) (*Cell, error) {
	return nil, ErrAugmentationSemanticsUnavailable
}

func (readOnlyTestAugmentation) CombineExtra(leftExtra, rightExtra *Slice) (*Cell, error) {
	return nil, ErrAugmentationSemanticsUnavailable
}

func TestAugmentedDictionaryCombineEmpty(t *testing.T) {
	aug := testMetricAugmentation{}

	left := mustNewCombineTestAugDict(t, 8, aug)
	right := mustNewCombineTestAugDict(t, 8, aug)

	ok, err := left.CombineWith(right)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if !ok {
		t.Fatal("combine returned conflict for empty dictionaries")
	}
	if !left.IsEmpty() {
		t.Fatal("left dictionary is not empty after empty merge")
	}

	extra, err := left.LoadRootExtra()
	if err != nil {
		t.Fatalf("failed to load root extra: %v", err)
	}
	if got := mustLoadTestMetricExtra(t, extra); got != 0 {
		t.Fatalf("unexpected empty root extra: %d", got)
	}
}

func TestAugmentedDictionaryCombineEmptyTakesOtherRoot(t *testing.T) {
	aug := testMetricAugmentation{}

	left := mustNewCombineTestAugDict(t, 8, aug)
	right := mustNewCombineTestAugDict(t, 8, aug)
	mustSetCombineTestValue(t, right, 8, 0x10, 0xaa, 8)

	wantRootHash := append([]byte(nil), right.root.Hash()...)
	wantExtraHash := append([]byte(nil), right.rootExtra.Hash()...)

	ok, err := left.CombineWith(right)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if !ok {
		t.Fatal("combine returned conflict")
	}
	if left.root != right.root {
		t.Fatal("empty receiver did not reuse other root")
	}
	if !bytes.Equal(left.root.Hash(), wantRootHash) {
		t.Fatal("combined root hash differs from other root hash")
	}
	if !bytes.Equal(left.rootExtra.Hash(), wantExtraHash) {
		t.Fatal("combined root extra hash differs from other root extra hash")
	}
}

func TestAugmentedDictionaryCombineDisjointSingleLeafs(t *testing.T) {
	aug := testMetricAugmentation{}

	left := mustNewCombineTestAugDict(t, 8, aug)
	right := mustNewCombineTestAugDict(t, 8, aug)
	mustSetCombineTestValue(t, left, 8, 0x10, 0xaa, 8)
	mustSetCombineTestValue(t, right, 8, 0x20, 0xbb, 8)

	ok, err := left.CombineWith(right)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if !ok {
		t.Fatal("combine returned conflict")
	}

	assertCombineTestValue(t, left, 8, 0x10, 0xaa, 8)
	assertCombineTestValue(t, left, 8, 0x20, 0xbb, 8)
	assertCombineTestRootExtra(t, left, 16)
}

func TestAugmentedDictionaryCombineDuplicateKeepsReceiver(t *testing.T) {
	aug := testMetricAugmentation{}

	left := mustNewCombineTestAugDict(t, 8, aug)
	right := mustNewCombineTestAugDict(t, 8, aug)
	mustSetCombineTestValue(t, left, 8, 0x10, 0xaa, 8)
	mustSetCombineTestValue(t, right, 8, 0x10, 0xbb, 8)

	beforeRoot := left.root
	beforeExtra := left.rootExtra
	beforeCell, err := left.ToCell()
	if err != nil {
		t.Fatalf("failed to serialize left dict: %v", err)
	}
	beforeHash := append([]byte(nil), beforeCell.Hash()...)

	ok, err := left.CombineWith(right)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if ok {
		t.Fatal("combine succeeded for duplicate key")
	}
	if left.root != beforeRoot {
		t.Fatal("receiver root changed after duplicate conflict")
	}
	if left.rootExtra != beforeExtra {
		t.Fatal("receiver root extra changed after duplicate conflict")
	}

	afterCell, err := left.ToCell()
	if err != nil {
		t.Fatalf("failed to serialize left dict after conflict: %v", err)
	}
	if !bytes.Equal(afterCell.Hash(), beforeHash) {
		t.Fatal("receiver serialized hash changed after duplicate conflict")
	}
	assertCombineTestValue(t, left, 8, 0x10, 0xaa, 8)
}

func TestAugmentedDictionaryCombineMultiLevelOverlappingPrefixes(t *testing.T) {
	aug := testMetricAugmentation{}

	left := mustNewCombineTestAugDict(t, 8, aug)
	right := mustNewCombineTestAugDict(t, 8, aug)
	leftKeys := []uint64{0x80, 0x81, 0x90, 0xa0}
	rightKeys := []uint64{0x82, 0x83, 0x91, 0xf0}

	for _, key := range leftKeys {
		mustSetCombineTestValue(t, left, 8, key, key, 8)
	}
	for _, key := range rightKeys {
		mustSetCombineTestValue(t, right, 8, key, key, 8)
	}

	ok, err := left.CombineWith(right)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if !ok {
		t.Fatal("combine returned conflict")
	}

	for _, key := range append(leftKeys, rightKeys...) {
		assertCombineTestValue(t, left, 8, key, key, 8)
	}
	assertCombineTestRootExtra(t, left, 64)
}

func TestAugmentedDictionaryCombineRootExtraRoundTrip(t *testing.T) {
	aug := testMetricAugmentation{}

	left := mustNewCombineTestAugDict(t, 8, aug)
	right := mustNewCombineTestAugDict(t, 8, aug)
	for _, key := range []uint64{0x01, 0x02, 0x40} {
		mustSetCombineTestValue(t, left, 8, key, key, 8)
	}
	for _, key := range []uint64{0x03, 0x41, 0x80} {
		mustSetCombineTestValue(t, right, 8, key, key, 8)
	}

	ok, err := left.CombineWith(right)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if !ok {
		t.Fatal("combine returned conflict")
	}
	assertCombineTestRootExtra(t, left, 48)

	wrapped, err := left.ToCell()
	if err != nil {
		t.Fatalf("failed to serialize wrapped dict: %v", err)
	}
	loaded, err := wrapped.BeginParse().LoadAugDict(8, aug, false)
	if err != nil {
		t.Fatalf("failed to load combined wrapped dict: %v", err)
	}
	assertCombineTestRootExtra(t, loaded, 48)
	for _, key := range []uint64{0x01, 0x02, 0x03, 0x40, 0x41, 0x80} {
		assertCombineTestValue(t, loaded, 8, key, key, 8)
	}
}

func TestAugmentedDictionaryCombineMatchesRangeExtraSetWithMode(t *testing.T) {
	aug := testMetricAugmentation{}

	left := mustNewCombineTestAugDict(t, 8, aug)
	right := mustNewCombineTestAugDict(t, 8, aug)
	for _, key := range []uint64{0x01, 0x02, 0x40, 0x80} {
		mustSetCombineTestValue(t, left, 8, key, key, 8)
	}
	for _, key := range []uint64{0x03, 0x41, 0x81, 0xf0} {
		mustSetCombineTestValue(t, right, 8, key, key, 8)
	}

	structural := left.Copy()
	ok, err := structural.CombineWith(right)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if !ok {
		t.Fatal("combine returned conflict")
	}

	legacy := left.Copy()
	items, err := right.RangeExtra(false, false)
	if err != nil {
		t.Fatalf("range failed: %v", err)
	}
	for _, item := range items {
		ok, err := legacy.SetWithMode(item.Key, item.Value.ToBuilder().EndCell(), DictSetModeAdd)
		if err != nil {
			t.Fatalf("legacy set failed: %v", err)
		}
		if !ok {
			t.Fatal("legacy set returned conflict")
		}
	}

	structuralCell, err := structural.ToCell()
	if err != nil {
		t.Fatalf("failed to serialize structural dict: %v", err)
	}
	legacyCell, err := legacy.ToCell()
	if err != nil {
		t.Fatalf("failed to serialize legacy dict: %v", err)
	}
	if !bytes.Equal(structuralCell.Hash(), legacyCell.Hash()) {
		t.Fatal("structural combine hash differs from RangeExtra+SetWithMode hash")
	}
}

func TestAugmentedDictionaryCombineLoadsLazyPayloadRefs(t *testing.T) {
	aug := testMetricAugmentation{}

	left := mustNewCombineTestAugDict(t, 8, aug)
	right := mustNewCombineTestAugDict(t, 8, aug)

	if err := left.Set(mustCombineTestKey(t, 8, 0x10), mustCombineTestValue(0xaa, 8)); err != nil {
		t.Fatalf("set left value: %v", err)
	}

	rightValueRef := BeginCell().MustStoreUInt(0xcc, 8).EndCell()
	rightValue := BeginCell().MustStoreUInt(0xbb, 8).MustStoreRef(rightValueRef).EndCell()
	if err := right.Set(mustCombineTestKey(t, 8, 0x20), rightValue); err != nil {
		t.Fatalf("set right value: %v", err)
	}

	loader := testLazyLoaderForCellTree(right.root)

	lazyRight := &AugmentedDictionary{
		keySz:     right.keySz,
		root:      cellWithLazyRefsFromCell(right.root, loader.LoadCell),
		rootExtra: right.rootExtra,
		wrapped:   right.wrapped,
		aug:       aug,
	}

	ok, err := left.CombineWith(lazyRight)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if !ok {
		t.Fatal("combine returned conflict")
	}
	if hasLazyPrunedRefs(left.root, map[Hash]struct{}{}) {
		t.Fatal("combined dictionary kept lazy pruned refs in materialized nodes")
	}

	assertCombineTestValue(t, left, 8, 0x10, 0xaa, 8)
	loaded, err := left.LoadValue(mustCombineTestKey(t, 8, 0x20))
	if err != nil {
		t.Fatalf("load right value: %v", err)
	}
	if got := mustLoadCombineTestValue(t, loaded, 8); got != 0xbb {
		t.Fatalf("unexpected right value: got %x, want bb", got)
	}
	if loaded.RefsNum() != 1 {
		t.Fatalf("unexpected right value refs: got %d, want 1", loaded.RefsNum())
	}
}

func TestAugmentedDictionaryCombineReadOnlyAugmentation(t *testing.T) {
	aug := testMetricAugmentation{}

	left := mustNewCombineTestAugDict(t, 8, aug)
	right := mustNewCombineTestAugDict(t, 8, aug)
	mustSetCombineTestValue(t, left, 8, 0x10, 0xaa, 8)
	mustSetCombineTestValue(t, right, 8, 0x20, 0xbb, 8)

	readOnly := &AugmentedDictionary{
		keySz:     left.keySz,
		root:      left.root,
		rootExtra: left.rootExtra,
		wrapped:   left.wrapped,
		aug:       readOnlyTestAugmentation{},
	}

	ok, err := readOnly.CombineWith(right)
	if err == nil {
		t.Fatal("combine with read-only augmentation succeeded")
	}
	if !errors.Is(err, ErrAugmentationSemanticsUnavailable) {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("combine returned ok with read-only augmentation")
	}
}

func BenchmarkAugmentedDictionaryCombine(b *testing.B) {
	aug := testMetricAugmentation{}
	const keySz = 16
	const keys = 2048

	left := mustBuildCombineBenchDict(b, keySz, 0, 2, keys, aug)
	right := mustBuildCombineBenchDict(b, keySz, 1, 2, keys, aug)

	b.Run("CombineWith", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			merged := left.Copy()
			ok, err := merged.CombineWith(right)
			if err != nil {
				b.Fatalf("combine failed: %v", err)
			}
			if !ok {
				b.Fatal("combine returned conflict")
			}
		}
	})

	b.Run("RangeExtraSetWithMode", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			merged := left.Copy()
			items, err := right.RangeExtra(false, false)
			if err != nil {
				b.Fatalf("range failed: %v", err)
			}
			for _, item := range items {
				ok, err := merged.SetWithMode(item.Key, item.Value.ToBuilder().EndCell(), DictSetModeAdd)
				if err != nil {
					b.Fatalf("set failed: %v", err)
				}
				if !ok {
					b.Fatal("set returned conflict")
				}
			}
		}
	})
}

func mustNewCombineTestAugDict(tb testing.TB, keySz uint, aug Augmentation) *AugmentedDictionary {
	tb.Helper()

	dict, err := NewAugDict(keySz, aug)
	if err != nil {
		tb.Fatalf("failed to create augmented dictionary: %v", err)
	}

	return dict
}

func mustSetCombineTestValue(tb testing.TB, dict *AugmentedDictionary, keySz uint, key uint64, value uint64, valueBits uint) {
	tb.Helper()

	if err := dict.Set(mustCombineTestKey(tb, keySz, key), mustCombineTestValue(value, valueBits)); err != nil {
		tb.Fatalf("failed to set value: %v", err)
	}
}

func assertCombineTestValue(tb testing.TB, dict *AugmentedDictionary, keySz uint, key uint64, value uint64, valueBits uint) {
	tb.Helper()

	loaded, err := dict.LoadValue(mustCombineTestKey(tb, keySz, key))
	if err != nil {
		tb.Fatalf("failed to load value for key %x: %v", key, err)
	}
	if got := mustLoadCombineTestValue(tb, loaded, valueBits); got != value {
		tb.Fatalf("unexpected value for key %x: got %x, want %x", key, got, value)
	}
}

func assertCombineTestRootExtra(tb testing.TB, dict *AugmentedDictionary, want uint64) {
	tb.Helper()

	extra, err := dict.LoadRootExtra()
	if err != nil {
		tb.Fatalf("failed to load root extra: %v", err)
	}
	if got := mustLoadCombineTestMetricExtra(tb, extra); got != want {
		tb.Fatalf("unexpected root extra: got %d, want %d", got, want)
	}
}

func mustBuildCombineBenchDict(tb testing.TB, keySz uint, start uint64, step uint64, count int, aug Augmentation) *AugmentedDictionary {
	tb.Helper()

	dict := mustNewCombineTestAugDict(tb, keySz, aug)
	for i := 0; i < count; i++ {
		key := start + uint64(i)*step
		mustSetCombineBenchValue(tb, dict, keySz, key, 16)
	}

	return dict
}

func mustSetCombineBenchValue(tb testing.TB, dict *AugmentedDictionary, keySz uint, key uint64, valueBits uint) {
	tb.Helper()

	value := BeginCell().MustStoreUInt(key, valueBits).EndCell()
	if err := dict.Set(mustCombineTestKey(tb, keySz, key), value); err != nil {
		tb.Fatalf("failed to set bench value: %v", err)
	}
}

func mustCombineTestKey(tb testing.TB, keySz uint, key uint64) *Cell {
	tb.Helper()
	return BeginCell().MustStoreUInt(key, keySz).EndCell()
}

func mustCombineTestValue(value uint64, bits uint) *Cell {
	return BeginCell().MustStoreUInt(value, bits).EndCell()
}

func mustLoadCombineTestValue(tb testing.TB, value *Slice, bits uint) uint64 {
	tb.Helper()

	loaded, err := value.LoadUInt(bits)
	if err != nil {
		tb.Fatalf("failed to load value: %v", err)
	}

	return loaded
}

func mustLoadCombineTestMetricExtra(tb testing.TB, extra *Slice) uint64 {
	tb.Helper()

	loaded, err := extra.LoadUInt(16)
	if err != nil {
		tb.Fatalf("failed to load metric extra: %v", err)
	}

	return loaded
}

func testLazyLoaderForCellTree(root *Cell) *testLazyLoader {
	cells := map[Hash]*Cell{}
	collectTestCellTree(root, cells)
	return &testLazyLoader{cells: cells}
}

func collectTestCellTree(root *Cell, cells map[Hash]*Cell) {
	if root == nil {
		return
	}
	hash := root.HashKey()
	if _, ok := cells[hash]; ok {
		return
	}

	cells[hash] = root
	cells[root.HashKey(0)] = root
	cells[root.HashKey(root.Level())] = root
	for _, ref := range root.rawRefs() {
		collectTestCellTree(ref, cells)
	}
}

func hasLazyPrunedRefs(root *Cell, visited map[Hash]struct{}) bool {
	if root == nil {
		return false
	}
	hash := root.HashKey()
	if _, ok := visited[hash]; ok {
		return false
	}
	visited[hash] = struct{}{}

	for _, ref := range root.rawRefs() {
		if ref.IsLazy() || hasLazyPrunedRefs(ref, visited) {
			return true
		}
	}
	return false
}
