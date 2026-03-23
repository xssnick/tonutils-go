package cell

import (
	"errors"
	"testing"
)

type testMetricAugmentation struct{}

func (testMetricAugmentation) SkipExtra(loader *Slice) error {
	_, err := loader.LoadUInt(16)
	return err
}

func (testMetricAugmentation) EmptyExtra() (*Cell, error) {
	return BeginCell().MustStoreUInt(0, 16).EndCell(), nil
}

func (testMetricAugmentation) LeafExtra(value *Slice) (*Cell, error) {
	metric := uint64(value.BitsLeft()) + uint64(value.RefsNum())*257
	return BeginCell().MustStoreUInt(metric, 16).EndCell(), nil
}

func (testMetricAugmentation) CombineExtra(leftExtra, rightExtra *Slice) (*Cell, error) {
	left, err := leftExtra.LoadUInt(16)
	if err != nil {
		return nil, err
	}
	right, err := rightExtra.LoadUInt(16)
	if err != nil {
		return nil, err
	}
	return BeginCell().MustStoreUInt(left+right, 16).EndCell(), nil
}

func mustTestAugKey(t *testing.T, value uint64) *Cell {
	t.Helper()
	return BeginCell().MustStoreUInt(value, 8).EndCell()
}

func mustTestAugValue(t *testing.T, value uint64, bits uint) *Cell {
	t.Helper()
	return BeginCell().MustStoreUInt(value, bits).EndCell()
}

func mustLoadTestMetricExtra(t *testing.T, extra *Slice) uint64 {
	t.Helper()
	value, err := extra.LoadUInt(16)
	if err != nil {
		t.Fatal(err)
	}
	if extra.BitsLeft() != 0 || extra.RefsNum() != 0 {
		t.Fatalf("unexpected trailing data in extra: %d bits, %d refs", extra.BitsLeft(), extra.RefsNum())
	}
	return value
}

func mustLoadTestValue(t *testing.T, value *Slice, bits uint) uint64 {
	t.Helper()
	got, err := value.LoadUInt(bits)
	if err != nil {
		t.Fatal(err)
	}
	if value.BitsLeft() != 0 || value.RefsNum() != 0 {
		t.Fatalf("unexpected trailing value data: %d bits, %d refs", value.BitsLeft(), value.RefsNum())
	}
	return got
}

func TestAugmentedDictionary_SetLoadDelete(t *testing.T) {
	aug := testMetricAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}

	key1 := mustTestAugKey(t, 0x10)
	key2 := mustTestAugKey(t, 0x11)
	val1 := mustTestAugValue(t, 0xaa, 8)
	val2 := mustTestAugValue(t, 0xbb, 8)

	changed, err := dict.SetWithMode(key1, val1, DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to set first key: changed=%v err=%v", changed, err)
	}

	changed, err = dict.SetWithMode(key1, val2, DictSetModeAdd)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("DictSetModeAdd should not replace an existing key")
	}

	changed, err = dict.SetWithMode(key2, val2, DictSetModeReplace)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("DictSetModeReplace should not insert a missing key")
	}

	changed, err = dict.SetWithMode(key2, val2, DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to set second key: changed=%v err=%v", changed, err)
	}

	rootExtra, err := dict.LoadRootExtra()
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, rootExtra); metric != 16 {
		t.Fatalf("unexpected root extra metric: %d", metric)
	}

	value, extra, err := dict.LoadValueExtra(key1)
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, extra); metric != 8 {
		t.Fatalf("unexpected key1 extra metric: %d", metric)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0xaa {
		t.Fatalf("unexpected key1 value: %x", got)
	}

	removed, err := dict.LoadValueAndDelete(key2)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, removed, 8); got != 0xbb {
		t.Fatalf("unexpected deleted value: %x", got)
	}

	rootExtra, err = dict.LoadRootExtra()
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, rootExtra); metric != 8 {
		t.Fatalf("unexpected root extra metric after delete: %d", metric)
	}

	if _, err = dict.LoadValueAndDelete(key2); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("expected ErrNoSuchKeyInDict on second delete, got %v", err)
	}

	loaded := dict.MustToCell().BeginParse().MustLoadAugDictWithAugmentation(8, aug)
	roundtrip, err := loaded.LoadValue(key1)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, roundtrip, 8); got != 0xaa {
		t.Fatalf("unexpected roundtrip value: %x", got)
	}
}

func TestAugmentedDictionary_SetRefAndBuilder(t *testing.T) {
	aug := testMetricAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}

	key1 := mustTestAugKey(t, 0x80)
	key2 := mustTestAugKey(t, 0x81)
	refValue := BeginCell().MustStoreUInt(0xfeed, 16).EndCell()

	changed, err := dict.SetRefWithMode(key1, refValue, DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to set ref value: changed=%v err=%v", changed, err)
	}

	ref, extra, err := dict.LoadValueRefExtra(key1)
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, extra); metric != 257 {
		t.Fatalf("unexpected ref extra metric: %d", metric)
	}
	if !equalCellContents(ref, refValue) {
		t.Fatal("loaded ref value does not match stored ref")
	}

	builderValue := BeginCell().MustStoreUInt(0x12, 8).MustStoreRef(BeginCell().MustStoreUInt(1, 1).EndCell())
	changed, err = dict.SetBuilderWithMode(key2, builderValue, DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to set builder value: changed=%v err=%v", changed, err)
	}

	rootExtra, err := dict.LoadRootExtra()
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, rootExtra); metric != 522 {
		t.Fatalf("unexpected combined root metric: %d", metric)
	}

	changed, err = dict.SetRefWithMode(key1, BeginCell().EndCell(), DictSetModeAdd)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("DictSetModeAdd should not replace an existing ref value")
	}

	replacement := mustTestAugValue(t, 0x33, 8)
	changed, err = dict.SetWithMode(key1, replacement, DictSetModeReplace)
	if err != nil || !changed {
		t.Fatalf("failed to replace ref value with inline value: changed=%v err=%v", changed, err)
	}

	rootExtra, err = dict.LoadRootExtra()
	if err != nil {
		t.Fatal(err)
	}
	if metric := mustLoadTestMetricExtra(t, rootExtra); metric != 273 {
		t.Fatalf("unexpected root metric after replace: %d", metric)
	}

	value, err := dict.LoadValue(key1)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0x33 {
		t.Fatalf("unexpected replaced value: %x", got)
	}

	if _, err = dict.LoadValueRef(key1); err == nil {
		t.Fatal("expected single-ref extraction to fail for inline value")
	}
}

func TestAugmentedDictionary_ReadOnlyLoader(t *testing.T) {
	aug := testMetricAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}

	key := mustTestAugKey(t, 0x55)
	val := mustTestAugValue(t, 0xcc, 8)
	if _, err = dict.SetWithMode(key, val, DictSetModeSet); err != nil {
		t.Fatal(err)
	}

	loaded := dict.MustToCell().BeginParse().MustLoadAugDict(8, func(loader *Slice) error {
		_, err := loader.LoadUInt(16)
		return err
	})

	value, err := loaded.LoadValue(key)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0xcc {
		t.Fatalf("unexpected value from read-only loader: %x", got)
	}

	extra := loaded.GetRootExtra()
	if extra == nil {
		t.Fatal("root extra should still be readable for read-only loader")
	}
	if metric := mustLoadTestMetricExtra(t, extra.BeginParse()); metric != 8 {
		t.Fatalf("unexpected read-only root extra metric: %d", metric)
	}

	if _, err = loaded.SetWithMode(key, val, DictSetModeSet); !errors.Is(err, ErrAugmentationSemanticsUnavailable) {
		t.Fatalf("expected read-only augmented dict mutation to fail with ErrAugmentationSemanticsUnavailable, got %v", err)
	}
}
