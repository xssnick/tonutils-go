package cell

import "testing"

func mustCollectAugKeys(t *testing.T, items []AugDictItem, bits uint) []uint64 {
	t.Helper()

	keys := make([]uint64, len(items))
	for i, item := range items {
		keys[i] = item.Key.BeginParse().MustLoadUInt(bits)
	}
	return keys
}

func TestAugmentedDictionary_RangeNearestCheckAndTraverse(t *testing.T) {
	aug := testMetricAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}

	for _, pair := range []struct {
		key uint64
		val uint64
	}{
		{0x10, 0xa1},
		{0x11, 0xb2},
		{0x80, 0xc3},
	} {
		if _, err = dict.SetWithMode(mustTestAugKey(t, pair.key), mustTestAugValue(t, pair.val, 8), DictSetModeSet); err != nil {
			t.Fatal(err)
		}
	}

	rawItems, err := dict.Range(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(rawItems); got != 3 {
		t.Fatalf("unexpected raw range length: %d", got)
	}

	items, err := dict.RangeExtra(false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCollectAugKeys(t, items, 8); !equalUint64Slices(got, []uint64{0x80, 0x10, 0x11}) {
		t.Fatalf("unexpected signed aug range order: %v", got)
	}
	for _, item := range items {
		if metric := mustLoadTestMetricExtra(t, item.Extra.Copy()); metric != 8 {
			t.Fatalf("unexpected item extra metric: %d", metric)
		}
	}

	key, valueExtra, err := dict.LookupNearestKey(mustTestAugKey(t, 0x12), false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := key.BeginParse().MustLoadUInt(8); got != 0x11 {
		t.Fatalf("unexpected nearest key: %x", got)
	}
	value, extra, err := dict.decomposeValueExtra(valueExtra)
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, extra); metric != 8 {
		t.Fatalf("unexpected nearest extra metric: %d", metric)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0xb2 {
		t.Fatalf("unexpected nearest value: %x", got)
	}

	var foreachCount int
	ok, err := dict.CheckForEach(func(valueExtra *Slice, key *Cell) (bool, error) {
		foreachCount++
		_, _, err := dict.decomposeValueExtra(valueExtra)
		return err == nil && key != nil, err
	}, false, false)
	if err != nil || !ok {
		t.Fatalf("check_for_each failed: ok=%v err=%v", ok, err)
	}
	if foreachCount != 3 {
		t.Fatalf("unexpected raw foreach count: %d", foreachCount)
	}

	var extraForeachCount int
	ok, err = dict.CheckForEachExtra(func(value, extra *Slice, key *Cell) (bool, error) {
		extraForeachCount++
		return key != nil && mustLoadTestMetricExtra(t, extra.Copy()) == 8 && value != nil, nil
	}, false)
	if err != nil || !ok {
		t.Fatalf("check_for_each_extra failed: ok=%v err=%v", ok, err)
	}
	if extraForeachCount != 3 {
		t.Fatalf("unexpected extra foreach count: %d", extraForeachCount)
	}

	foundValue, foundExtra, err := dict.TraverseExtra(func(keyPrefix *Cell, extra *Slice, value *Slice) (int, error) {
		if value == nil {
			return 6, nil
		}
		if keyPrefix.BeginParse().MustLoadUInt(8) == 0x11 {
			return 1, nil
		}
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, foundExtra); metric != 8 {
		t.Fatalf("unexpected traversed extra metric: %d", metric)
	}
	if got := mustLoadTestValue(t, foundValue, 8); got != 0xb2 {
		t.Fatalf("unexpected traversed value: %x", got)
	}

	ok, err = dict.ValidateCheck(func(_ *Slice, _ *Cell) (bool, error) {
		return true, nil
	}, false)
	if err != nil || !ok {
		t.Fatalf("validate_check failed: ok=%v err=%v", ok, err)
	}
	ok, err = dict.ValidateCheckExtra(func(_ *Slice, _ *Slice, _ *Cell) (bool, error) {
		return true, nil
	}, false)
	if err != nil || !ok {
		t.Fatalf("validate_check_extra failed: ok=%v err=%v", ok, err)
	}
	if !dict.ValidateAll() {
		t.Fatal("validate_all failed for valid augmented dict")
	}
}

func TestAugmentedDictionary_FilterAndPrefixCut(t *testing.T) {
	aug := testMetricAugmentation{}
	dict, err := NewAugDict(4, aug)
	if err != nil {
		t.Fatal(err)
	}

	for _, pair := range []struct {
		key uint64
		val uint64
	}{
		{0b1000, 0x10},
		{0b1001, 0x11},
		{0b0011, 0x03},
	} {
		if _, err = dict.SetWithMode(BeginCell().MustStoreUInt(pair.key, 4).EndCell(), mustTestAugValue(t, pair.val, 8), DictSetModeSet); err != nil {
			t.Fatal(err)
		}
	}

	subRoot, err := dict.ExtractPrefixSubdictRoot(BeginCell().MustStoreUInt(0b10, 2).EndCell(), true)
	if err != nil {
		t.Fatal(err)
	}
	sub := subRoot.AsAugDict(2, aug)
	subItems, err := sub.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCollectAugKeys(t, subItems, 2); !equalUint64Slices(got, []uint64{0b00, 0b01}) {
		t.Fatalf("unexpected extracted aug subdict keys: %v", got)
	}

	ok, err := dict.CutPrefixSubdict(BeginCell().MustStoreUInt(0b10, 2).EndCell(), true)
	if err != nil || !ok {
		t.Fatalf("failed to cut augmented prefix subdict: ok=%v err=%v", ok, err)
	}
	if dict.GetKeySize() != 2 {
		t.Fatalf("unexpected aug key size after cut: %d", dict.GetKeySize())
	}

	rootExtra, err := dict.LoadRootExtra()
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, rootExtra); metric != 16 {
		t.Fatalf("unexpected root extra after cut: %d", metric)
	}

	changes, err := dict.Filter(func(value, _ *Slice, _ *Cell) (DictFilterAction, error) {
		if mustLoadTestValue(t, value.Copy(), 8) == 0x11 {
			return DictFilterRemove, nil
		}
		return DictFilterKeep, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Fatalf("unexpected augmented filter changes: %d", changes)
	}

	rootExtra, err = dict.LoadRootExtra()
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, rootExtra); metric != 8 {
		t.Fatalf("unexpected root extra after filter: %d", metric)
	}
}
