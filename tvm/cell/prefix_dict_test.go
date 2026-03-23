package cell

import (
	"errors"
	"strings"
	"testing"
)

func mustPrefixKey(t *testing.T, value uint64, bits uint) *Cell {
	t.Helper()
	return BeginCell().MustStoreUInt(value, bits).EndCell()
}

func mustPrefixValue(t *testing.T, value uint64, bits uint) *Cell {
	t.Helper()
	return BeginCell().MustStoreUInt(value, bits).EndCell()
}

func TestPrefixDictionary_LookupPrefix(t *testing.T) {
	dict := NewPrefixDict(8)

	if err := dict.Set(mustPrefixKey(t, 0b10, 2), mustPrefixValue(t, 0xaa, 8)); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(mustPrefixKey(t, 0b011, 3), mustPrefixValue(t, 0xbb, 8)); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(mustPrefixKey(t, 0b1110, 4), mustPrefixValue(t, 0xcc, 8)); err != nil {
		t.Fatal(err)
	}

	value, matched, err := dict.LookupPrefix(mustPrefixKey(t, 0b101100, 6))
	if err != nil {
		t.Fatal(err)
	}
	if matched != 2 {
		t.Fatalf("unexpected matched prefix length: %d", matched)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0xaa {
		t.Fatalf("unexpected prefix value: %x", got)
	}

	exact, err := dict.LoadValue(mustPrefixKey(t, 0b011, 3))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, exact, 8); got != 0xbb {
		t.Fatalf("unexpected exact value: %x", got)
	}

	if _, err = dict.LoadValue(mustPrefixKey(t, 0b101100, 6)); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("expected exact lookup miss, got %v", err)
	}

	value, matched, err = dict.LookupPrefix(mustPrefixKey(t, 0b1111, 4))
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		t.Fatal("expected mismatch inside edge to return nil value")
	}
	if matched != 3 {
		t.Fatalf("unexpected mismatch prefix length: %d", matched)
	}
}

func TestPrefixDictionary_SetModesAndForkCollision(t *testing.T) {
	dict := NewPrefixDict(8)

	changed, err := dict.SetWithMode(mustPrefixKey(t, 0b10, 2), mustPrefixValue(t, 0x01, 8), DictSetModeReplace)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("replace should not insert missing key")
	}

	changed, err = dict.SetWithMode(mustPrefixKey(t, 0b10, 2), mustPrefixValue(t, 0x02, 8), DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to insert key: changed=%v err=%v", changed, err)
	}

	changed, err = dict.SetWithMode(mustPrefixKey(t, 0b10, 2), mustPrefixValue(t, 0x03, 8), DictSetModeAdd)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("add should not replace an existing key")
	}

	changed, err = dict.SetWithMode(mustPrefixKey(t, 0b10, 2), mustPrefixValue(t, 0x04, 8), DictSetModeReplace)
	if err != nil || !changed {
		t.Fatalf("failed to replace key: changed=%v err=%v", changed, err)
	}

	value, err := dict.LoadValue(mustPrefixKey(t, 0b10, 2))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0x04 {
		t.Fatalf("unexpected replaced value: %x", got)
	}

	if err = dict.Set(mustPrefixKey(t, 0b11, 2), mustPrefixValue(t, 0x05, 8)); err != nil {
		t.Fatal(err)
	}

	changed, err = dict.SetWithMode(mustPrefixKey(t, 0b1, 1), mustPrefixValue(t, 0x06, 8), DictSetModeSet)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("cannot insert a value into an existing fork prefix")
	}

	if _, err = dict.LoadValue(mustPrefixKey(t, 0b1, 1)); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("expected missing exact key at fork, got %v", err)
	}

	builderValue := BeginCell().MustStoreUInt(0x77, 8)
	changed, err = dict.SetBuilderWithMode(mustPrefixKey(t, 0b001, 3), builderValue, DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to insert builder value: changed=%v err=%v", changed, err)
	}

	builderLoaded, err := dict.LoadValue(mustPrefixKey(t, 0b001, 3))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, builderLoaded, 8); got != 0x77 {
		t.Fatalf("unexpected builder value: %x", got)
	}

	changed, err = dict.SetBuilderWithMode(mustPrefixKey(t, 0b001, 3), BeginCell().MustStoreUInt(0x66, 8), DictSetModeAdd)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("builder add should not replace an existing key")
	}
}

func TestPrefixDictionary_DeleteMergesEdges(t *testing.T) {
	dict := NewPrefixDict(8)

	if err := dict.Set(mustPrefixKey(t, 0b100, 3), mustPrefixValue(t, 0xa1, 8)); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(mustPrefixKey(t, 0b101, 3), mustPrefixValue(t, 0xb2, 8)); err != nil {
		t.Fatal(err)
	}

	removed, err := dict.LoadValueAndDelete(mustPrefixKey(t, 0b100, 3))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, removed, 8); got != 0xa1 {
		t.Fatalf("unexpected deleted value: %x", got)
	}

	remaining, err := dict.LoadValue(mustPrefixKey(t, 0b101, 3))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, remaining, 8); got != 0xb2 {
		t.Fatalf("unexpected surviving value: %x", got)
	}

	root := dict.AsCell()
	if root == nil {
		t.Fatal("dict root should not be nil after deleting one of two keys")
	}

	loader := root.BeginParse()
	labelLen, label, err := loadLabel(8, loader, BeginCell())
	if err != nil {
		t.Fatal(err)
	}
	if labelLen != 3 {
		t.Fatalf("unexpected merged label length: %d", labelLen)
	}
	if got := mustLoadTestValue(t, label.ToSlice(), 3); got != 0b101 {
		t.Fatalf("unexpected merged label bits: %b", got)
	}
	if isFork := loader.MustLoadBoolBit(); isFork {
		t.Fatal("merged root should be a leaf")
	}
	if got := mustLoadTestValue(t, loader, 8); got != 0xb2 {
		t.Fatalf("unexpected merged root value: %x", got)
	}

	stored := BeginCell().MustStoreDict(dict).EndCell()
	roundtrip := stored.BeginParse().MustLoadPrefixDict(8)
	value, err := roundtrip.LoadValue(mustPrefixKey(t, 0b101, 3))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0xb2 {
		t.Fatalf("unexpected roundtrip value: %x", got)
	}

	if _, err = dict.LoadValueAndDelete(mustPrefixKey(t, 0b100, 3)); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("expected ErrNoSuchKeyInDict on second delete, got %v", err)
	}
}

func TestLoadCell_LoadPrefixDictRejectsMalformedForks(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0, 2).MustStoreBoolBit(false).EndCell()

	rootZeroRemaining := BeginCell()
	if err := storeDictLabel(rootZeroRemaining, BeginCell().MustStoreUInt(0, 1).ToSlice(), 1); err != nil {
		t.Fatal(err)
	}
	rootZeroRemaining.MustStoreBoolBit(true).MustStoreRef(leaf).MustStoreRef(leaf)

	_, err := BeginCell().MustStoreMaybeRef(rootZeroRemaining.EndCell()).EndCell().BeginParse().LoadPrefixDict(1)
	if err == nil || !strings.Contains(err.Error(), "zero remaining key length") {
		t.Fatalf("expected zero remaining key length error, got %v", err)
	}

	validLeaf := BeginCell().MustStoreUInt(0, 2).MustStoreBoolBit(false).EndCell()
	rootExtraBits := BeginCell().
		MustStoreUInt(0, 2).
		MustStoreBoolBit(true).
		MustStoreBoolBit(true).
		MustStoreRef(validLeaf).
		MustStoreRef(validLeaf).
		EndCell()

	_, err = BeginCell().MustStoreMaybeRef(rootExtraBits).EndCell().BeginParse().LoadPrefixDict(1)
	if err == nil || !strings.Contains(err.Error(), "invalid fork node in a prefix code dictionary") {
		t.Fatalf("expected malformed fork error, got %v", err)
	}
}
