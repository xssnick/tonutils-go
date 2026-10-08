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

func TestPrefixDictionary_SliceKeyViewSemantics(t *testing.T) {
	dict := NewPrefixDict(8)
	if err := dict.Set(mustPrefixKey(t, 0b10, 2), mustPrefixValue(t, 0xaa, 8)); err != nil {
		t.Fatal(err)
	}

	// a key slice carrying trailing bits and refs: the lookup clamps to the key
	// size and must leave the caller's refs alone
	source := BeginCell().MustStoreUInt(0b1011001101, 10).
		MustStoreRef(BeginCell().MustStoreUInt(1, 8).EndCell()).EndCell()

	key := source.MustBeginParse()
	var value Slice
	matched, err := dict.LookupPrefixBySliceInto(key, &value)
	if err != nil {
		t.Fatal(err)
	}
	if matched != 2 {
		t.Fatalf("matched = %d, want 2", matched)
	}
	if got := mustLoadTestValue(t, &value, 8); got != 0xaa {
		t.Fatalf("value = %#x, want 0xaa", got)
	}
	if key.BitsLeft() != 10 || key.RefsNum() != 1 {
		t.Fatalf("caller key consumed: %d bits, %d refs left", key.BitsLeft(), key.RefsNum())
	}

	// the mutating entry points reject an over-long key instead of clamping it
	if _, err = dict.SetBuilderBySliceKeyWithMode(source.MustBeginParse(), BeginCell().MustStoreUInt(0xbb, 8), DictSetModeSet); err == nil {
		t.Fatal("set accepted a key longer than the key size")
	}
	if _, err = dict.LoadValueAndDeleteBySliceKey(source.MustBeginParse()); err == nil {
		t.Fatal("delete accepted a key longer than the key size")
	}

	// a within-size key slice mutates and must not consume the caller's refs
	shortSource := BeginCell().MustStoreUInt(0b011, 3).
		MustStoreRef(BeginCell().MustStoreUInt(2, 8).EndCell()).EndCell()

	setKey := shortSource.MustBeginParse()
	changed, err := dict.SetBuilderBySliceKeyWithMode(setKey, BeginCell().MustStoreUInt(0xbb, 8), DictSetModeSet)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("set reported no change")
	}
	if setKey.BitsLeft() != 3 || setKey.RefsNum() != 1 {
		t.Fatalf("caller set key consumed: %d bits, %d refs left", setKey.BitsLeft(), setKey.RefsNum())
	}

	deleteKey := shortSource.MustBeginParse()
	deleted, err := dict.LoadValueAndDeleteBySliceKey(deleteKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, deleted, 8); got != 0xbb {
		t.Fatalf("deleted value = %#x, want 0xbb", got)
	}
	if deleteKey.BitsLeft() != 3 || deleteKey.RefsNum() != 1 {
		t.Fatalf("caller delete key consumed: %d bits, %d refs left", deleteKey.BitsLeft(), deleteKey.RefsNum())
	}
	if _, err = dict.LoadValue(mustPrefixKey(t, 0b011, 3)); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("key survived delete: %v", err)
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

	loader := root.MustBeginParse()
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
	roundtrip := stored.MustBeginParse().MustLoadPrefixDict(8)
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

func TestPrefixDictionary_DeleteResolvesSurvivorLibrary(t *testing.T) {
	const keySize = 4

	leftKey := mustPrefixKey(t, 0, 1)
	rightKey := mustPrefixKey(t, 1, 1)
	leftValue := mustPrefixValue(t, 0xa1, 8)
	rightValue := mustPrefixValue(t, 0xb2, 8)

	seed := NewPrefixDict(keySize)
	if err := seed.Set(leftKey, leftValue); err != nil {
		t.Fatal(err)
	}
	if err := seed.Set(rightKey, rightValue); err != nil {
		t.Fatal(err)
	}

	rootNode, err := parseFixedDictNode(seed.root, keySize)
	if err != nil {
		t.Fatal(err)
	}
	resolvedSurvivor, err := rootNode.ref(1)
	if err != nil {
		t.Fatal(err)
	}
	rootWithLibrary, _, err := rootNode.cloneWithRef(1, libraryDictNode(t), nil)
	if err != nil {
		t.Fatal(err)
	}

	resolver := newFixedDictCellResolver(resolvedSurvivor)
	dict := (&PrefixDictionary{keySz: keySize, root: rootWithLibrary}).SetTrace(resolver.trace)
	removed, err := dict.LoadValueAndDelete(leftKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, removed, 8); got != 0xa1 {
		t.Fatalf("deleted value = %#x, want 0xa1", got)
	}

	expected := NewPrefixDict(keySize)
	if err = expected.Set(rightKey, rightValue); err != nil {
		t.Fatal(err)
	}
	if dict.root.HashKey() != expected.root.HashKey() {
		t.Fatalf("merged root hash = %x, want %x", dict.root.HashKey(), expected.root.HashKey())
	}
	value, err := dict.LoadValue(rightKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0xb2 {
		t.Fatalf("surviving value = %#x, want 0xb2", got)
	}
}

func TestLoadCell_LoadPrefixDictDefersMalformedForkValidation(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0, 2).MustStoreBoolBit(false).EndCell()

	rootZeroRemaining := BeginCell()
	if err := storeDictLabel(rootZeroRemaining, BeginCell().MustStoreUInt(0, 1).ToSlice(), 1); err != nil {
		t.Fatal(err)
	}
	rootZeroRemaining.MustStoreBoolBit(true).MustStoreRef(leaf).MustStoreRef(leaf)

	zeroRemaining := rootZeroRemaining.EndCell()
	dict, err := BeginCell().MustStoreMaybeRef(zeroRemaining).EndCell().MustBeginParse().LoadPrefixDict(1)
	if err != nil {
		t.Fatalf("LoadPrefixDict should not recursively validate branches: %v", err)
	}
	if err = validatePrefixDictRoot(dict.root, dict.keySz); err == nil || !strings.Contains(err.Error(), "zero remaining key length") {
		t.Fatalf("expected zero remaining key length error during explicit validation, got %v", err)
	}

	validLeaf := BeginCell().MustStoreUInt(0, 2).MustStoreBoolBit(false).EndCell()
	rootExtraBits := BeginCell().
		MustStoreUInt(0, 2).
		MustStoreBoolBit(true).
		MustStoreBoolBit(true).
		MustStoreRef(validLeaf).
		MustStoreRef(validLeaf).
		EndCell()

	dict, err = BeginCell().MustStoreMaybeRef(rootExtraBits).EndCell().MustBeginParse().LoadPrefixDict(1)
	if err != nil {
		t.Fatalf("LoadPrefixDict should not recursively validate branches: %v", err)
	}
	if err = validatePrefixDictRoot(dict.root, dict.keySz); err == nil || !strings.Contains(err.Error(), "invalid fork node in a prefix code dictionary") {
		t.Fatalf("expected malformed fork error during explicit validation, got %v", err)
	}
}
