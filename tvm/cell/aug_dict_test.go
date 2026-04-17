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

	changed, err := dict.SetBuilderWithMode(key1, BeginCell().MustStoreRef(refValue), DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to set ref value: changed=%v err=%v", changed, err)
	}

	refValueSlice, extra, err := dict.LoadValueExtra(key1)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := loadSingleRefValue(refValueSlice)
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

	changed, err = dict.SetBuilderWithMode(key1, BeginCell().MustStoreRef(BeginCell().EndCell()), DictSetModeAdd)
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

	value, err = dict.LoadValue(key1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loadSingleRefValue(value); err == nil {
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

func TestAugmentedDictionary_ToAugDictWithValueAndAugmentation_LeafRefValueKeepsTail(t *testing.T) {
	aug := testMetricAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}

	key := mustTestAugKey(t, 0x44)
	refValue := BeginCell().MustStoreUInt(0xbeef, 16).EndCell()
	if _, err = dict.SetBuilderWithMode(key, BeginCell().MustStoreRef(refValue), DictSetModeSet); err != nil {
		t.Fatal(err)
	}

	container := BeginCell().
		MustStoreUInt(0b101, 3).
		MustStoreBuilder(dict.root.ToBuilder()).
		MustStoreUInt(0x1d, 5).
		EndCell()

	loader := container.BeginParse()
	if got := loader.MustLoadUInt(3); got != 0b101 {
		t.Fatalf("unexpected prefix bits: %b", got)
	}

	loaded, err := loader.ToAugDictWithValueAndAugmentation(8, aug, func(loader *Slice) error {
		_, err := loader.LoadRefCell()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	valueSlice, extra, err := loaded.LoadValueExtra(key)
	if err != nil {
		t.Fatal(err)
	}
	value, err := loadSingleRefValue(valueSlice)
	if err != nil {
		t.Fatal(err)
	}
	if !equalCellContents(value, refValue) {
		t.Fatal("loaded ref value does not match original ref")
	}
	if metric := mustLoadTestMetricExtra(t, extra); metric != 257 {
		t.Fatalf("unexpected leaf extra metric: %d", metric)
	}

	if metric := mustLoadTestMetricExtra(t, loaded.GetRootExtra().BeginParse()); metric != 257 {
		t.Fatalf("unexpected root extra metric: %d", metric)
	}

	if got := loader.MustLoadUInt(5); got != 0x1d {
		t.Fatalf("unexpected tail bits: %x", got)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		t.Fatalf("unexpected trailing data after inline augmented dict: %d bits, %d refs", loader.BitsLeft(), loader.RefsNum())
	}
}

func TestAugmentedDictionary_ToAugDictWithValueAndAugmentation_ForkRootKeepsTail(t *testing.T) {
	aug := testMetricAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}

	key1 := mustTestAugKey(t, 0x10)
	key2 := mustTestAugKey(t, 0x11)
	val1 := mustTestAugValue(t, 0xaa, 8)
	val2 := mustTestAugValue(t, 0xbb, 8)

	if _, err = dict.SetWithMode(key1, val1, DictSetModeSet); err != nil {
		t.Fatal(err)
	}
	if _, err = dict.SetWithMode(key2, val2, DictSetModeSet); err != nil {
		t.Fatal(err)
	}

	container := BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreBuilder(dict.root.ToBuilder()).
		MustStoreUInt(0x2a, 6).
		EndCell()

	loader := container.BeginParse()
	if got := loader.MustLoadUInt(2); got != 0b11 {
		t.Fatalf("unexpected prefix bits: %b", got)
	}

	loaded, err := loader.ToAugDictWithValueAndAugmentation(8, aug, func(loader *Slice) error {
		_, err := loader.LoadUInt(8)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	value1, err := loaded.LoadValue(key1)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, value1, 8); got != 0xaa {
		t.Fatalf("unexpected first loaded value: %x", got)
	}

	value2, err := loaded.LoadValue(key2)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, value2, 8); got != 0xbb {
		t.Fatalf("unexpected second loaded value: %x", got)
	}

	if metric := mustLoadTestMetricExtra(t, loaded.GetRootExtra().BeginParse()); metric != 16 {
		t.Fatalf("unexpected fork root extra metric: %d", metric)
	}

	if got := loader.MustLoadUInt(6); got != 0x2a {
		t.Fatalf("unexpected tail bits: %x", got)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		t.Fatalf("unexpected trailing data after fork inline augmented dict: %d bits, %d refs", loader.BitsLeft(), loader.RefsNum())
	}
}
