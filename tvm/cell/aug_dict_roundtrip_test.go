package cell

import (
	"errors"
	"testing"
)

func mustBuildTestAugDict(t *testing.T, keys ...uint64) *AugmentedDictionary {
	t.Helper()

	dict, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if err = dict.Set(mustTestAugKey(t, key), mustTestAugValue(t, key+1, 16)); err != nil {
			t.Fatal(err)
		}
	}
	return dict
}

// The HashmapAugE cell produced by ToCell is `1 root:^HashmapAug extra:Y`, not a
// HashmapAug node. Handing it to the inline loader used to be accepted silently
// and only surfaced later as an unrelated "invalid dictionary fork node" during
// a trie walk.
func TestAugmentedDictionary_ToCellIsRejectedByInlineLoader(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []uint64
	}{
		{name: "empty"},
		{name: "leaf root", keys: []uint64{0x10}},
		{name: "fork root", keys: []uint64{0x10, 0x11, 0x12, 0x80}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped, err := mustBuildTestAugDict(t, tc.keys...).ToCell()
			if err != nil {
				t.Fatal(err)
			}

			_, err = wrapped.MustBeginParse().ToAugDictWithAugmentation(8, testMetricAugmentation{})
			if !errors.Is(err, ErrNotInlineAugDictNode) {
				t.Fatalf("expected ErrNotInlineAugDictNode, got %v", err)
			}
		})
	}
}

// ToCell pairs with LoadAugDict, RootCell pairs with ToAugDictWithAugmentation.
func TestAugmentedDictionary_RoundTripsThroughMatchingLoader(t *testing.T) {
	for _, count := range []int{0, 1, 2, 5, 32} {
		keys := make([]uint64, count)
		for i := range keys {
			keys[i] = uint64(i) * 7
		}
		dict := mustBuildTestAugDict(t, keys...)

		wrapped, err := dict.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		reloaded, err := wrapped.MustBeginParse().LoadAugDict(8, testMetricAugmentation{}, false)
		if err != nil {
			t.Fatalf("count %d: LoadAugDict: %v", count, err)
		}
		assertTestAugDictContent(t, reloaded, keys)

		if count == 0 {
			// a HashmapAug node has no empty form, so there is nothing inline to read
			if dict.RootCell() != nil {
				t.Fatal("empty augmented dict must not have a root node")
			}
			continue
		}

		inline, err := dict.RootCell().MustBeginParse().ToAugDictWithAugmentation(8, testMetricAugmentation{})
		if err != nil {
			t.Fatalf("count %d: ToAugDictWithAugmentation: %v", count, err)
		}
		assertTestAugDictContent(t, inline, keys)

		back, err := inline.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		if !equalCellContents(back, dict.RootCell()) {
			t.Fatalf("count %d: inline re-serialization diverged", count)
		}
	}
}

// A fork root carries no value, so with a nil skipValue it must end exactly at
// its extra: trailing payload would be swallowed into the root node cell.
func TestAugmentedDictionary_InlineForkRootRejectsTrailingData(t *testing.T) {
	dict := mustBuildTestAugDict(t, 0x10, 0x11)

	container := BeginCell().
		MustStoreBuilder(dict.RootCell().ToBuilder()).
		MustStoreUInt(0x2a, 6).
		EndCell()

	_, err := container.MustBeginParse().ToAugDictWithAugmentation(8, testMetricAugmentation{})
	if !errors.Is(err, ErrNotInlineAugDictNode) {
		t.Fatalf("expected ErrNotInlineAugDictNode, got %v", err)
	}
}

func assertTestAugDictContent(t *testing.T, dict *AugmentedDictionary, keys []uint64) {
	t.Helper()

	items, err := dict.RangeExtra(false, false)
	if err != nil {
		t.Fatalf("RangeExtra: %v", err)
	}
	if len(items) != len(keys) {
		t.Fatalf("got %d items, want %d", len(items), len(keys))
	}

	for _, key := range keys {
		value, err := dict.LoadValue(mustTestAugKey(t, key))
		if err != nil {
			t.Fatalf("LoadValue(%d): %v", key, err)
		}
		if got := mustLoadTestValue(t, value, 16); got != key+1 {
			t.Fatalf("LoadValue(%d) = %d", key, got)
		}
	}
}
