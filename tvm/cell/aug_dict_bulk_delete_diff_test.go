package cell

import (
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
