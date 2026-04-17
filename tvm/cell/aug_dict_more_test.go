package cell

import (
	"errors"
	"math/big"
	"testing"
)

type failingAugmentation struct {
	leafErr    error
	combineErr error
	emptyErr   error
}

func (a failingAugmentation) SkipExtra(loader *Slice) error {
	_, err := loader.LoadUInt(16)
	return err
}

func (a failingAugmentation) EmptyExtra() (*Cell, error) {
	if a.emptyErr != nil {
		return nil, a.emptyErr
	}
	return BeginCell().MustStoreUInt(0, 16).EndCell(), nil
}

func (a failingAugmentation) LeafExtra(value *Slice) (*Cell, error) {
	if a.leafErr != nil {
		return nil, a.leafErr
	}
	return testMetricAugmentation{}.LeafExtra(value)
}

func (a failingAugmentation) CombineExtra(leftExtra, rightExtra *Slice) (*Cell, error) {
	if a.combineErr != nil {
		return nil, a.combineErr
	}
	return testMetricAugmentation{}.CombineExtra(leftExtra, rightExtra)
}

func mustUIntFromCell(t *testing.T, c *Cell, bits uint) uint64 {
	t.Helper()
	if c == nil {
		t.Fatal("cell is nil")
	}
	return c.BeginParse().MustLoadUInt(bits)
}

func TestAugmentedDictionary_WrappersIteratorsAndInlineLoaders(t *testing.T) {
	aug := testMetricAugmentation{}

	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	if dict.GetKeySize() != 8 || !dict.IsEmpty() {
		t.Fatalf("unexpected new dict state: keySz=%d empty=%v", dict.GetKeySize(), dict.IsEmpty())
	}

	emptyCell, err := dict.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	loadedEmpty, err := emptyCell.BeginParse().LoadAugDictWithAugmentation(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	if !loadedEmpty.IsEmpty() {
		t.Fatal("empty wrapped augmented dict should stay empty after roundtrip")
	}
	if metric := mustLoadTestMetricExtra(t, loadedEmpty.GetRootExtra().BeginParse()); metric != 0 {
		t.Fatalf("unexpected empty root metric: %d", metric)
	}

	if err = dict.Set(mustTestAugKey(t, 0x01), mustTestAugValue(t, 0x11, 8)); err != nil {
		t.Fatal(err)
	}
	if err = dict.SetIntKey(big.NewInt(2), mustTestAugValue(t, 0x22, 8)); err != nil {
		t.Fatal(err)
	}
	refValue := BeginCell().MustStoreUInt(0x3333, 16).EndCell()
	if err = dict.SetBuilder(mustTestAugKey(t, 0x03), BeginCell().MustStoreRef(refValue)); err != nil {
		t.Fatal(err)
	}
	if err = dict.SetBuilder(mustTestAugKey(t, 0x04), BeginCell().MustStoreUInt(0x44, 8)); err != nil {
		t.Fatal(err)
	}

	if got := mustUIntFromCell(t, dict.Get(mustTestAugKey(t, 0x01)), 8); got != 0x11 {
		t.Fatalf("unexpected Get value: %x", got)
	}
	if got := mustUIntFromCell(t, func() *Cell {
		val, err := dict.LoadValueByIntKey(big.NewInt(2))
		if err != nil {
			t.Fatal(err)
		}
		c, err := val.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		return c
	}(), 8); got != 0x22 {
		t.Fatalf("unexpected GetByIntKey value: %x", got)
	}
	if dict.GetWithExtra(mustTestAugKey(t, 0x01)) == nil {
		t.Fatal("GetWithExtra should return a cell")
	}

	value, err := dict.LoadValueByIntKey(big.NewInt(2))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0x22 {
		t.Fatalf("unexpected LoadValueByIntKey value: %x", got)
	}

	valueExtra, err := dict.LoadValueWithExtraByIntKey(big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	value, extra, err := dict.decomposeValueExtra(valueExtra)
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, extra); metric != 8 {
		t.Fatalf("unexpected inline extra: %d", metric)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0x11 {
		t.Fatalf("unexpected inline value: %x", got)
	}

	value, extra, err = dict.LoadValueExtraByIntKey(big.NewInt(2))
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, extra); metric != 8 {
		t.Fatalf("unexpected extra by int key: %d", metric)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0x22 {
		t.Fatalf("unexpected value extra by int key: %x", got)
	}

	refValueSlice, err := dict.LoadValueByIntKey(big.NewInt(3))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := loadSingleRefValue(refValueSlice)
	if err != nil {
		t.Fatal(err)
	}
	if !equalCellContents(ref, refValue) {
		t.Fatal("unexpected ref loaded by int key")
	}

	refValueSlice, extra, err = dict.LoadValueExtraByIntKey(big.NewInt(3))
	if err != nil {
		t.Fatal(err)
	}
	ref, err = loadSingleRefValue(refValueSlice)
	if err != nil {
		t.Fatal(err)
	}
	if !equalCellContents(ref, refValue) {
		t.Fatal("unexpected ref+extra loaded by int key")
	}
	if metric := mustLoadTestMetricExtra(t, extra); metric != 257 {
		t.Fatalf("unexpected ref extra by int key: %d", metric)
	}

	it, err := dict.Iterator(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if item := it.Item(); item.Key != nil || item.Value != nil {
		t.Fatal("iterator item before Next should be empty")
	}
	var iterKeys []uint64
	for it.Next() {
		iterKeys = append(iterKeys, it.Key().BeginParse().MustLoadUInt(8))
		if it.Value() == nil {
			t.Fatal("iterator value should not be nil")
		}
	}
	if !equalUint64Slices(iterKeys, []uint64{0x01, 0x02, 0x03, 0x04}) {
		t.Fatalf("unexpected iterator order: %v", iterKeys)
	}
	it.Reset()
	if !it.Next() || it.Key().BeginParse().MustLoadUInt(8) != 0x01 {
		t.Fatal("iterator Reset should rewind iteration")
	}

	var nilDictIt *DictIterator
	if nilDictIt.Next() {
		t.Fatal("nil dict iterator should not iterate")
	}
	nilDictIt.Reset()
	if item := nilDictIt.Item(); item.Key != nil || item.Value != nil || nilDictIt.Value() != nil {
		t.Fatal("nil dict iterator item should stay empty")
	}

	extraIt, err := dict.IteratorExtra(true, false)
	if err != nil {
		t.Fatal(err)
	}
	if item := extraIt.Item(); item.Key != nil || item.Value != nil || item.Extra != nil {
		t.Fatal("augmented iterator item before Next should be empty")
	}
	var reverseKeys []uint64
	for extraIt.Next() {
		reverseKeys = append(reverseKeys, extraIt.Key().BeginParse().MustLoadUInt(8))
		if extraIt.Value() == nil || extraIt.Extra() == nil {
			t.Fatal("augmented iterator should expose value and extra")
		}
	}
	if !equalUint64Slices(reverseKeys, []uint64{0x04, 0x03, 0x02, 0x01}) {
		t.Fatalf("unexpected augmented iterator order: %v", reverseKeys)
	}
	extraIt.Reset()
	if !extraIt.Next() || extraIt.Key().BeginParse().MustLoadUInt(8) != 0x04 {
		t.Fatal("augmented iterator Reset should rewind iteration")
	}

	var nilAugIt *AugDictIterator
	if nilAugIt.Next() {
		t.Fatal("nil augmented iterator should not iterate")
	}
	nilAugIt.Reset()
	if item := nilAugIt.Item(); item.Key != nil || item.Value != nil || item.Extra != nil || nilAugIt.Key() != nil || nilAugIt.Value() != nil || nilAugIt.Extra() != nil {
		t.Fatal("nil augmented iterator item should stay empty")
	}

	items, err := dict.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	cloned := cloneAugDictItems(items)
	if _, err = items[0].Value.LoadUInt(1); err != nil {
		t.Fatal(err)
	}
	if cloned[0].Value.BitsLeft() != 8 {
		t.Fatalf("cloned augmented items should preserve slice snapshots, got %d bits", cloned[0].Value.BitsLeft())
	}

	ok, err := dict.HasCommonPrefix(BeginCell().MustStoreUInt(0, 5).EndCell())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected common prefix on small sequential keys")
	}
	ok, err = dict.HasCommonPrefix(BeginCell().MustStoreUInt(1, 5).EndCell())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unexpected common prefix match")
	}
	common, err := dict.GetCommonPrefix(5)
	if err != nil {
		t.Fatal(err)
	}
	if common.BitsSize() != 5 || common.BeginParse().MustLoadUInt(5) != 0 {
		t.Fatalf("unexpected common prefix: %s", common.Dump())
	}

	semanticInline, err := dict.root.BeginParse().ToAugDictWithAugmentation(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := semanticInline.LoadValue(mustTestAugKey(t, 0x01)); err != nil || mustLoadTestValue(t, got, 8) != 0x11 {
		t.Fatalf("unexpected semantic inline load: err=%v", err)
	}

	readOnlyInline, err := dict.root.BeginParse().ToAugDict(8, func(loader *Slice) error {
		_, err := loader.LoadUInt(16)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readOnlyInline.LoadValue(mustTestAugKey(t, 0x02)); err != nil || mustLoadTestValue(t, got, 8) != 0x22 {
		t.Fatalf("unexpected readonly inline load: err=%v", err)
	}

	leafDict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	if err = leafDict.Set(mustTestAugKey(t, 0x05), mustTestAugValue(t, 0x55, 8)); err != nil {
		t.Fatal(err)
	}
	container := BeginCell().
		MustStoreUInt(0b101, 3).
		MustStoreBuilder(leafDict.root.ToBuilder()).
		MustStoreUInt(0x1d, 5).
		EndCell()
	loader := container.BeginParse()
	if got := loader.MustLoadUInt(3); got != 0b101 {
		t.Fatalf("unexpected container prefix: %b", got)
	}
	inlineWithTail, err := loader.ToAugDictWithValue(8, func(loader *Slice) error {
		_, err := loader.LoadUInt(16)
		return err
	}, func(loader *Slice) error {
		_, err := loader.LoadUInt(8)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := inlineWithTail.LoadValueByIntKey(big.NewInt(5)); err != nil || mustLoadTestValue(t, got, 8) != 0x55 {
		t.Fatalf("unexpected inline-with-tail load: err=%v", err)
	}
	if got := loader.MustLoadUInt(5); got != 0x1d {
		t.Fatalf("unexpected inline tail bits: %x", got)
	}
}

func TestAugmentedDictionary_DeleteFilterAndHelperErrorPaths(t *testing.T) {
	aug := testMetricAugmentation{}

	if _, err := NewAugDict(8, nil); err == nil {
		t.Fatal("NewAugDict should reject nil augmentation")
	}

	ro := readOnlyAugmentation{}
	if err := ro.SkipExtra(BeginCell().EndCell().BeginParse()); err == nil {
		t.Fatal("readOnlyAugmentation without skipper should fail")
	}
	if _, err := ro.LeafExtra(BeginCell().EndCell().BeginParse()); !errors.Is(err, ErrAugmentationSemanticsUnavailable) {
		t.Fatalf("unexpected LeafExtra error: %v", err)
	}
	if _, err := ro.CombineExtra(BeginCell().EndCell().BeginParse(), BeginCell().EndCell().BeginParse()); !errors.Is(err, ErrAugmentationSemanticsUnavailable) {
		t.Fatalf("unexpected CombineExtra error: %v", err)
	}

	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(mustTestAugKey(t, 0x10), mustTestAugValue(t, 0xa0, 8)); err != nil {
		t.Fatal(err)
	}
	if err = dict.SetIntKey(big.NewInt(17), mustTestAugValue(t, 0xb1, 8)); err != nil {
		t.Fatal(err)
	}
	refValue := BeginCell().MustStoreUInt(0xcafe, 16).EndCell()
	if err = dict.SetBuilder(mustTestAugKey(t, 0x12), BeginCell().MustStoreRef(refValue)); err != nil {
		t.Fatal(err)
	}

	value, extra, err := dict.LoadValueExtraAndDelete(mustTestAugKey(t, 0x10))
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, extra); metric != 8 {
		t.Fatalf("unexpected deleted inline extra: %d", metric)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0xa0 {
		t.Fatalf("unexpected deleted inline value: %x", got)
	}

	refValueSlice, err := dict.LoadValueAndDelete(mustTestAugKey(t, 0x12))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := loadSingleRefValue(refValueSlice)
	if err != nil {
		t.Fatal(err)
	}
	if !equalCellContents(ref, refValue) {
		t.Fatal("unexpected deleted ref value")
	}

	if err = dict.SetBuilder(mustTestAugKey(t, 0x12), BeginCell().MustStoreRef(refValue)); err != nil {
		t.Fatal(err)
	}
	refValueSlice, extra, err = dict.LoadValueExtraAndDelete(mustTestAugKey(t, 0x12))
	if err != nil {
		t.Fatal(err)
	}
	ref, err = loadSingleRefValue(refValueSlice)
	if err != nil {
		t.Fatal(err)
	}
	if !equalCellContents(ref, refValue) {
		t.Fatal("unexpected deleted ref+extra value")
	}
	if metric := mustLoadTestMetricExtra(t, extra); metric != 257 {
		t.Fatalf("unexpected deleted ref extra: %d", metric)
	}

	if err = dict.DeleteIntKey(big.NewInt(17)); err != nil {
		t.Fatal(err)
	}
	if err = dict.Delete(mustTestAugKey(t, 0x17)); err != nil {
		t.Fatalf("missing delete should be ignored, got %v", err)
	}

	if err = dict.Set(mustTestAugKey(t, 0x20), mustTestAugValue(t, 0x20, 8)); err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(mustTestAugKey(t, 0x21), mustTestAugValue(t, 0x21, 8)); err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(mustTestAugKey(t, 0x22), mustTestAugValue(t, 0x22, 8)); err != nil {
		t.Fatal(err)
	}

	changes, err := dict.Filter(func(value, _ *Slice, key *Cell) (DictFilterAction, error) {
		switch key.BeginParse().MustLoadUInt(8) {
		case 0x20:
			return DictFilterKeepRest, nil
		case 0x21:
			return DictFilterRemoveRest, nil
		default:
			if mustLoadTestValue(t, value.Copy(), 8) == 0xb1 {
				return DictFilterRemove, nil
			}
			return DictFilterKeep, nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if changes != 0 {
		t.Fatalf("unexpected keep-rest changes: %d", changes)
	}

	changes, err = dict.Filter(func(_ *Slice, _ *Slice, key *Cell) (DictFilterAction, error) {
		if key.BeginParse().MustLoadUInt(8) == 0x21 {
			return DictFilterRemoveRest, nil
		}
		return DictFilterKeep, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if changes == 0 {
		t.Fatal("remove-rest filter should report changes")
	}

	if _, err = dict.Filter(func(_ *Slice, _ *Slice, _ *Cell) (DictFilterAction, error) {
		return DictFilterAction(255), nil
	}); err == nil {
		t.Fatal("unknown filter action should fail")
	}

	left, err := dict.storeLeaf(BeginCell().MustStoreUInt(0, 1).ToSlice(), BeginCell().MustStoreUInt(0xaa, 8), 1)
	if err != nil {
		t.Fatal(err)
	}
	right, err := dict.storeLeaf(BeginCell().MustStoreUInt(1, 1).ToSlice(), BeginCell().MustStoreUInt(0xbb, 8), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dict.storeFork(BeginCell().EndCell().BeginParse(), left, right, 2); err != nil {
		t.Fatal(err)
	}

	leftExtra, err := extractAugmentedNodeExtra(left, 1, aug.SkipExtra)
	if err != nil {
		t.Fatal(err)
	}
	rightExtra, err := extractAugmentedNodeExtra(right, 1, aug.SkipExtra)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = dict.storeForkWithExtra(BeginCell().EndCell().BeginParse(), nil, leftExtra, right, rightExtra, 2); err == nil {
		t.Fatal("fork with nil child should fail")
	}
	if _, _, err = dict.storeForkWithExtra(BeginCell().MustStoreUInt(0b11, 2).ToSlice(), left, leftExtra, right, rightExtra, 2); err == nil {
		t.Fatal("fork with overlong label should fail")
	}

	if got := dict.AsCell(); got == nil {
		t.Fatal("AsCell should expose wrapped dict cell")
	}
	if _, err = (&AugmentedDictionary{keySz: 8, wrapped: false}).ToCell(); err == nil {
		t.Fatal("inline empty augmented dict should be rejected")
	}
	if _, err = (&AugmentedDictionary{keySz: 8}).LoadRootExtra(); err == nil {
		t.Fatal("LoadRootExtra should fail without augmentation")
	}
	if _, err = (&AugmentedDictionary{keySz: 8}).ensureRootExtra(); err == nil {
		t.Fatal("ensureRootExtra should fail without augmentation")
	}
}

func TestAugmentedDictionary_ValidationAndNilAPICoverage(t *testing.T) {
	aug := testMetricAugmentation{}
	roAug := readOnlyAugmentation{skip: func(loader *Slice) error {
		_, err := loader.LoadUInt(16)
		return err
	}}

	empty, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	emptyCell, err := empty.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := emptyCell.BeginParse().LoadAugDictWithAugmentation(8, roAug); err != nil || !loaded.IsEmpty() {
		t.Fatalf("unexpected read-only empty load: loaded=%v err=%v", loaded, err)
	}
	if loaded := emptyCell.BeginParse().MustLoadAugDictWithAugmentation(8, roAug); !loaded.IsEmpty() {
		t.Fatal("MustLoadAugDictWithAugmentation should load empty wrapped dict")
	}

	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(mustTestAugKey(t, 0x40), mustTestAugValue(t, 0x40, 8)); err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(mustTestAugKey(t, 0x41), mustTestAugValue(t, 0x41, 8)); err != nil {
		t.Fatal(err)
	}

	wrappedCell, err := dict.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := wrappedCell.BeginParse().LoadAugDictWithAugmentation(8, roAug); err != nil || loaded.GetRootExtra() == nil {
		t.Fatalf("unexpected read-only wrapped load: loaded=%v err=%v", loaded, err)
	}
	if loaded := wrappedCell.BeginParse().MustLoadAugDict(8, roAug.SkipExtra); loaded.GetRootExtra() == nil {
		t.Fatal("MustLoadAugDict should expose wrapped root extra")
	}

	emptyInline := &AugmentedDictionary{keySz: 8, aug: aug}
	extra, err := emptyInline.ensureRootExtra()
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, extra.BeginParse()); metric != 0 {
		t.Fatalf("unexpected ensured empty extra: %d", metric)
	}
	inlineCopy := dict.Copy()
	inlineCopy.rootExtra = nil
	extra, err = inlineCopy.ensureRootExtra()
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, extra.BeginParse()); metric != 16 {
		t.Fatalf("unexpected ensured root extra: %d", metric)
	}

	if ok, err := (*AugmentedDictionary)(nil).CheckForEach(nil, false, false); err != nil || !ok {
		t.Fatalf("nil CheckForEach should succeed: ok=%v err=%v", ok, err)
	}
	if ok, err := (*AugmentedDictionary)(nil).ValidateCheck(nil, false); err != nil || !ok {
		t.Fatalf("nil ValidateCheck should succeed: ok=%v err=%v", ok, err)
	}
	if ok, err := (*AugmentedDictionary)(nil).CheckForEachExtra(nil, false); err != nil || !ok {
		t.Fatalf("nil CheckForEachExtra should succeed: ok=%v err=%v", ok, err)
	}
	if ok, err := (*AugmentedDictionary)(nil).ValidateCheckExtra(nil, false); err != nil || !ok {
		t.Fatalf("nil ValidateCheckExtra should succeed: ok=%v err=%v", ok, err)
	}
	if value, extra, err := (*AugmentedDictionary)(nil).TraverseExtra(nil); err != nil || value != nil || extra != nil {
		t.Fatalf("nil TraverseExtra should be empty: value=%v extra=%v err=%v", value, extra, err)
	}
	if root, err := (*AugmentedDictionary)(nil).ExtractPrefixSubdictRoot(nil, false); err != nil || root != nil {
		t.Fatalf("nil ExtractPrefixSubdictRoot should be empty: root=%v err=%v", root, err)
	}
	if ok, err := (*AugmentedDictionary)(nil).CutPrefixSubdict(nil, false); err != nil || !ok {
		t.Fatalf("nil CutPrefixSubdict should succeed: ok=%v err=%v", ok, err)
	}

	if ok, err := dict.CutPrefixSubdict(BeginCell().MustStoreUInt(0, 9).EndCell(), true); err != nil || ok {
		t.Fatalf("too-long augmented prefix cut should return false,nil: ok=%v err=%v", ok, err)
	}
	if root, err := dict.ExtractPrefixSubdictRoot(BeginCell().MustStoreUInt(0b11, 2).EndCell(), false); err != nil || root != nil {
		t.Fatalf("missing prefix without removal should return nil subdict: root=%v err=%v", root, err)
	}

	if _, _, err := dict.TraverseExtra(func(_ *Cell, _ *Slice, value *Slice) (int, error) {
		if value == nil {
			return 3, nil
		}
		return 0, nil
	}); err == nil {
		t.Fatal("invalid traverse directive should fail")
	}

	libCell, err := BeginCell().MustStoreUInt(uint64(LibraryCellType), 8).MustStoreSlice(make([]byte, 32), 256).EndCellSpecial(true)
	if err != nil {
		t.Fatal(err)
	}
	bad := &AugmentedDictionary{keySz: 8, root: libCell, aug: aug}
	if ok, err := bad.ValidateCheck(nil, false); err == nil || ok {
		t.Fatalf("ValidateCheck should fail on unsupported special cells: ok=%v err=%v", ok, err)
	}
	if _, _, err := bad.TraverseExtra(func(_ *Cell, _ *Slice, _ *Slice) (int, error) { return 0, nil }); err == nil {
		t.Fatal("TraverseExtra should fail on unsupported special cells")
	}

	pruned := makeManualCellForTest(true, LevelMask{Mask: 1}, 288, append([]byte{byte(PrunedCellType), 0x01}, make([]byte, 34)...), nil)
	if err := validateAugmentedDictNode(pruned, 8, aug.SkipExtra); err != nil {
		t.Fatalf("pruned augmented node should be accepted: %v", err)
	}
	if _, err := computeAugmentedNodeExtra(pruned, 8, aug); !errors.Is(err, ErrAugmentationSemanticsUnavailable) {
		t.Fatalf("expected pruned extra computation to report unavailable semantics, got %v", err)
	}
	if _, err := computeAugmentedNodeExtra(nil, 8, aug); err != nil {
		t.Fatalf("nil extra computation should use EmptyExtra, got %v", err)
	}
	if _, err := extractAugmentedNodeExtra(nil, 8, aug.SkipExtra); err == nil {
		t.Fatal("extractAugmentedNodeExtra should reject nil branches")
	}

	if !equalCellContents(nil, nil) {
		t.Fatal("equalCellContents(nil, nil) should be true")
	}
	if equalCellContents(BeginCell().EndCell(), BeginCell().MustStoreUInt(1, 1).EndCell()) {
		t.Fatal("different cells should not compare equal")
	}

	if _, err := captureConsumedPrefix(BeginCell().MustStoreUInt(0xaa, 8).EndCell().BeginParse(), func(*Slice) error {
		return errors.New("boom")
	}); err == nil {
		t.Fatal("captureConsumedPrefix should propagate consumer errors")
	}
}

func TestAugmentedDictionary_ErrorBranchesForConstructionAndWrapping(t *testing.T) {
	leafFail := failingAugmentation{leafErr: errors.New("leaf extra failed")}
	leafDict, err := NewAugDict(8, leafFail)
	if err != nil {
		t.Fatal(err)
	}
	if err = leafDict.Set(mustTestAugKey(t, 1), mustTestAugValue(t, 0x11, 8)); err == nil {
		t.Fatal("Set should fail when LeafExtra fails")
	}

	combineFail := failingAugmentation{combineErr: errors.New("combine extra failed")}
	forkDict, err := NewAugDict(8, combineFail)
	if err != nil {
		t.Fatal(err)
	}
	if err = forkDict.Set(mustTestAugKey(t, 0x10), mustTestAugValue(t, 0xaa, 8)); err != nil {
		t.Fatal(err)
	}
	if err = forkDict.Set(mustTestAugKey(t, 0x11), mustTestAugValue(t, 0xbb, 8)); err == nil {
		t.Fatal("second Set should fail when CombineExtra fails")
	}

	aug := testMetricAugmentation{}
	valid, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	if err = valid.Set(mustTestAugKey(t, 0x22), mustTestAugValue(t, 0x44, 8)); err != nil {
		t.Fatal(err)
	}

	badEmpty := BeginCell().MustStoreUInt(0, 1).MustStoreUInt(1, 16).EndCell()
	if _, err = badEmpty.BeginParse().LoadAugDictWithAugmentation(8, aug); err == nil {
		t.Fatal("wrapped empty augmented dict with bad extra should fail")
	}

	badRootExtra := BeginCell().MustStoreUInt(1, 1).MustStoreRef(valid.root).MustStoreUInt(0xffff, 16).EndCell()
	if _, err = badRootExtra.BeginParse().LoadAugDictWithAugmentation(8, aug); err == nil {
		t.Fatal("wrapped augmented dict with bad root extra should fail")
	}

	inline := &AugmentedDictionary{keySz: 8, root: valid.root, aug: aug, wrapped: false}
	if cell, err := inline.ToCell(); err != nil || cell != valid.root {
		t.Fatalf("inline ToCell should expose root directly: cell=%v err=%v", cell, err)
	}
	if cell := inline.MustToCell(); cell != valid.root {
		t.Fatal("inline MustToCell should expose root directly")
	}

	var nilCopy *AugmentedDictionary
	if nilCopy.Copy() != nil {
		t.Fatal("Copy on nil augmented dict should stay nil")
	}

	rootSetter := &AugmentedDictionary{keySz: 8, aug: aug, wrapped: true}
	if err := rootSetter.setRootWithExtra(nil, nil); err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, rootSetter.rootExtra.BeginParse()); metric != 0 {
		t.Fatalf("unexpected empty root extra after setRootWithExtra: %d", metric)
	}
	if err := rootSetter.setRootWithExtra(valid.root, nil); err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, rootSetter.rootExtra.BeginParse()); metric != 8 {
		t.Fatalf("unexpected extracted root extra after setRootWithExtra: %d", metric)
	}
}
