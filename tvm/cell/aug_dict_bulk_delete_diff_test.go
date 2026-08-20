package cell

import (
	"fmt"
	"math/rand"
	"testing"
)

// DeleteMany only earns its place if it is indistinguishable from the loop it
// replaces. Random batches run through DeleteMany and repeated Delete on
// copies of the same dictionary and must agree bit for bit — including full
// drains, single-survivor label merges and augmentation recombination.
func TestDeleteManyMatchesRepeatedDelete(t *testing.T) {
	rnd := rand.New(rand.NewSource(2026080902))
	for round := 0; round < 300; round++ {
		keyBits := []uint{8, 16, 32, 256, 352}[rnd.Intn(5)]
		maxKeys := 90
		if keyBits == 8 {
			maxKeys = 40
		}
		n := 1 + rnd.Intn(maxKeys)
		existing := randomBulkKeys(t, rnd, keyBits, n, nil)
		dict, err := NewAugDict(keyBits, bulkSumAugmentation{})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range existing {
			if err = dict.Set(key, bulkValue(key, 0)); err != nil {
				t.Fatal(err)
			}
		}
		del := 1 + rnd.Intn(len(existing))
		rnd.Shuffle(len(existing), func(i, j int) { existing[i], existing[j] = existing[j], existing[i] })
		batch := existing[:del]

		oneByOne := dict.Copy()
		for _, key := range batch {
			if err = oneByOne.Delete(key); err != nil {
				t.Fatalf("round %d: sequential delete: %v", round, err)
			}
		}
		bulk := dict.Copy()
		if err = bulk.DeleteMany(batch); err != nil {
			t.Fatalf("round %d: DeleteMany(%d of %d): %v", round, del, n, err)
		}

		want, err := oneByOne.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		got, err := bulk.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		if want.HashKey() != got.HashKey() {
			t.Fatalf("round %d: roots differ after deleting %d of %d (keyBits %d)", round, del, n, keyBits)
		}
		// A key that is gone must fail the whole batch.
		if err = bulk.DeleteMany(batch[:1]); err == nil {
			t.Fatalf("round %d: bulk delete of a removed key succeeded", round)
		}
	}
}

func BenchmarkAugDictDeleteManyParallel(b *testing.B) {
	const keyBits = 256
	rnd := rand.New(rand.NewSource(2026081903))
	keys := randomBulkKeys(b, rnd, keyBits, 100_000, nil)
	base, err := NewAugDict(keyBits, bulkSumAugmentation{})
	if err != nil {
		b.Fatal(err)
	}
	entries := make([]AugmentedEntry, len(keys))
	for i, key := range keys {
		entries[i] = AugmentedEntry{Key: key, Value: bulkValue(key, uint64(i%65_535+1))}
	}
	if err = base.SetMany(entries); err != nil {
		b.Fatal(err)
	}
	deleted := append([]*Cell(nil), keys[:1000]...)
	rnd.Shuffle(len(deleted), func(i, j int) { deleted[i], deleted[j] = deleted[j], deleted[i] })

	for _, parallelism := range []int{1, 8} {
		b.Run(fmt.Sprintf("parallel=%d", parallelism), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				dict := base.Copy()
				if err := dict.DeleteMany(deleted, parallelism); err != nil {
					b.Fatal(err)
				}
				if _, err := dict.ToCell(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
