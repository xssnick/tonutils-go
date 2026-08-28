package cell

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func TestAugmentedBulkSortMatchesStableComparisonSort(t *testing.T) {
	for _, tc := range []struct {
		keyBits uint
		items   int
	}{
		{keyBits: 0, items: 300},
		{keyBits: 7, items: 300},
		{keyBits: 8, items: dictBulkRadixThreshold - 1},
		{keyBits: 8, items: dictBulkRadixThreshold},
		{keyBits: 9, items: 1000},
		{keyBits: 17, items: 4096},
		{keyBits: 256, items: 4096},
		{keyBits: 1023, items: 512},
	} {
		t.Run(fmt.Sprintf("bits=%d/items=%d", tc.keyBits, tc.items), func(t *testing.T) {
			rnd := rand.New(rand.NewSource(int64(tc.keyBits)<<32 | int64(tc.items)))
			items, deletes := augmentedBulkSortFixture(t, rnd, tc.keyBits, tc.items)

			wantItems := append([]augBulkItem(nil), items...)
			sort.SliceStable(wantItems, func(i, j int) bool {
				return compareKeySlices(&wantItems[i].key, &wantItems[j].key) < 0
			})
			wantDeletes := append([]Slice(nil), deletes...)
			sort.SliceStable(wantDeletes, func(i, j int) bool {
				return compareKeySlices(&wantDeletes[i], &wantDeletes[j]) < 0
			})

			sortAugBulkItems(items, tc.keyBits)
			sortAugBulkDeleteKeys(deletes, tc.keyBits)

			for i := range items {
				if items[i].value != wantItems[i].value {
					t.Fatalf("write item %d changed stable ordering", i)
				}
				if deletes[i].cell != wantDeletes[i].cell {
					t.Fatalf("delete item %d changed stable ordering", i)
				}
			}
		})
	}
}

func TestAugmentedBulkRadixRejectsDuplicates(t *testing.T) {
	const keyBits = 17
	const count = 300

	value := BeginCell().MustStoreUInt(1, 1).EndCell()
	entries := make([]AugmentedEntry, count)
	keys := make([]*Cell, count)
	for i := range count - 1 {
		key := BeginCell().MustStoreUInt(uint64(i), keyBits).EndCell()
		keys[i] = key
		entries[i] = AugmentedEntry{Key: key, Value: value}
	}

	// Use a separately decoded cell for the duplicate so equality is decided by
	// the key bits, including the masked partial tail, rather than pointer reuse.
	duplicate, err := FromBOC(keys[37].ToBOC())
	if err != nil {
		t.Fatal(err)
	}
	keys[count-1] = duplicate
	entries[count-1] = AugmentedEntry{Key: duplicate, Value: value, Mode: DictSetModeReplace}

	rnd := rand.New(rand.NewSource(2026082701))
	rnd.Shuffle(count, func(i, j int) {
		entries[i], entries[j] = entries[j], entries[i]
		keys[i], keys[j] = keys[j], keys[i]
	})

	dict, err := NewAugDict(keyBits, bulkSumAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	if err = dict.SetMany(entries); err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("SetMany duplicate error = %v", err)
	}
	if err = dict.DeleteMany(keys); err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("DeleteMany duplicate error = %v", err)
	}
}

func BenchmarkAugmentedBulkSort(b *testing.B) {
	const keyBits = 256

	for _, count := range []int{256, 1024, 4096} {
		rnd := rand.New(rand.NewSource(2026082702 + int64(count)))
		fixture, _ := augmentedBulkSortFixture(b, rnd, keyBits, count)
		work := make([]augBulkItem, len(fixture))

		b.Run(fmt.Sprintf("items=%d/comparison", count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				copy(work, fixture)
				sort.Slice(work, func(i, j int) bool {
					return compareKeySlices(&work[i].key, &work[j].key) < 0
				})
			}
		})
		b.Run(fmt.Sprintf("items=%d/radix", count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				copy(work, fixture)
				sortAugBulkItems(work, keyBits)
			}
		})
	}
}

func augmentedBulkSortFixture(
	tb testing.TB,
	rnd *rand.Rand,
	keyBits uint,
	count int,
) ([]augBulkItem, []Slice) {
	tb.Helper()

	items := make([]augBulkItem, count)
	deletes := make([]Slice, count)
	markers := make([]Builder, count)
	keyBytes := make([]byte, (keyBits+7)/8)
	for i := range count {
		if _, err := rnd.Read(keyBytes); err != nil {
			tb.Fatal(err)
		}
		if keyBits%8 != 0 && len(keyBytes) != 0 {
			keyBytes[len(keyBytes)-1] &= byte(0xFF << (8 - keyBits%8))
		}

		key := BeginCell()
		if err := key.StoreSlice(keyBytes, keyBits); err != nil {
			tb.Fatal(err)
		}
		cell := key.EndCell()
		if err := cell.BeginParseInto(&deletes[i]); err != nil {
			tb.Fatal(err)
		}
		items[i].key = deletes[i]
		items[i].value = &markers[i]
	}
	return items, deletes
}
