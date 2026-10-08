package cell

import (
	"math/rand"
	"testing"
)

func TestSlice_CountInlineDictLeavesMatchesCount(t *testing.T) {
	rnd := rand.New(rand.NewSource(21))

	for round := 0; round < 12; round++ {
		count := 1 + rnd.Intn(40)
		dict, err := NewAugDict(64, testRefValueAug{})
		if err != nil {
			t.Fatal(err)
		}
		seen := map[uint64]bool{}
		inserted := 0
		for inserted < count {
			key := rnd.Uint64() & 0xffff
			if seen[key] {
				continue
			}
			seen[key] = true
			err = dict.Set(
				mustDictKey(t, key, 64),
				BeginCell().MustStoreRef(BeginCell().MustStoreUInt(key, 64).EndCell()).EndCell(),
			)
			if err != nil {
				t.Fatal(err)
			}
			inserted++
		}

		container := buildInlineAugDictCell(t, dict)
		loader := container.MustBeginParse()
		if _, err = loader.LoadUInt(4 + 16); err != nil {
			t.Fatal(err)
		}
		bitsBefore, refsBefore := loader.BitsLeft(), loader.RefsNum()

		got, err := loader.CountInlineDictLeaves(64)
		if err != nil {
			t.Fatalf("round=%d: %v", round, err)
		}
		expected, err := dict.Count()
		if err != nil {
			t.Fatal(err)
		}
		if got != expected || got != count {
			t.Fatalf("round=%d: count = %d, want %d (inserted %d)", round, got, expected, count)
		}
		if loader.BitsLeft() != bitsBefore || loader.RefsNum() != refsBefore {
			t.Fatalf("round=%d: slice was consumed: bits %d->%d refs %d->%d", round, bitsBefore, loader.BitsLeft(), refsBefore, loader.RefsNum())
		}
	}
}

func TestAugmentedDictionary_ForEachValueExtraMatchesIterator(t *testing.T) {
	rnd := rand.New(rand.NewSource(33))
	dict, err := NewAugDict(64, testRefValueAug{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		key := rnd.Uint64()
		err = dict.Set(
			mustDictKey(t, key, 64),
			BeginCell().MustStoreRef(BeginCell().MustStoreUInt(key, 64).EndCell()).EndCell(),
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	it, err := dict.IteratorExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	var expectedValues []uint64
	var expectedExtras []uint64
	for it.Next() {
		ref, err := it.Value().LoadRefCell()
		if err != nil {
			t.Fatal(err)
		}
		expectedValues = append(expectedValues, ref.MustBeginParse().MustLoadUInt(64))
		expectedExtras = append(expectedExtras, it.Extra().MustLoadUInt(8))
	}
	if err = it.Err(); err != nil {
		t.Fatal(err)
	}

	var gotValues []uint64
	var gotExtras []uint64
	err = dict.ForEachValueExtra(func(value, extra *Slice) (bool, error) {
		ref, err := value.LoadRefCell()
		if err != nil {
			return false, err
		}
		gotValues = append(gotValues, ref.MustBeginParse().MustLoadUInt(64))
		gotExtras = append(gotExtras, extra.MustLoadUInt(8))
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if !equalUint64Slices(gotValues, expectedValues) {
		t.Fatalf("values mismatch: got %x, want %x", gotValues, expectedValues)
	}
	if !equalUint64Slices(gotExtras, expectedExtras) {
		t.Fatalf("extras mismatch: got %x, want %x", gotExtras, expectedExtras)
	}

	// early stop
	visited := 0
	err = dict.ForEachValueExtra(func(_, _ *Slice) (bool, error) {
		visited++
		return visited < 5, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if visited != 5 {
		t.Fatalf("visited = %d, want 5", visited)
	}

	// nil dict is a no-op
	var nilDict *AugmentedDictionary
	if err = nilDict.ForEachValueExtra(func(_, _ *Slice) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestDictionaryForEachRefValueValidatesExactValues(t *testing.T) {
	dict := NewDict(8)
	for i, value := range []uint64{0xaa, 0xbb} {
		ref := BeginCell().MustStoreUInt(value, 8).EndCell()
		if err := dict.SetBuilder(
			mustDictKey(t, uint64(i+1), 8),
			BeginCell().MustStoreRef(ref),
		); err != nil {
			t.Fatal(err)
		}
	}

	var values []uint64
	count, err := dict.ForEachRefValue(func(value *Cell) error {
		values = append(values, value.MustBeginParse().MustLoadUInt(8))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || !equalUint64Slices(values, []uint64{0xaa, 0xbb}) {
		t.Fatalf("count=%d values=%x", count, values)
	}

	malformed := NewDict(8)
	if err = malformed.SetBuilder(
		mustDictKey(t, 1, 8),
		BeginCell().MustStoreBoolBit(true).MustStoreRef(BeginCell().EndCell()),
	); err != nil {
		t.Fatal(err)
	}
	if _, err = malformed.ForEachRefValue(nil); err == nil {
		t.Fatal("expected inline data in a one-reference value to be rejected")
	}
}
