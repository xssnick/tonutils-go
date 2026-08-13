package cell

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// SetMany only earns its place if it is indistinguishable from the loop it
// replaces. Every case below builds the same dictionary twice — once key by key,
// once in one batch — and compares the serialized roots, so a divergence in a
// label split, an edge bit or a recombined augmentation shows up as a hash
// mismatch rather than as a subtly wrong extra somewhere deep in the tree.
func TestSetManyMatchesRepeatedSet(t *testing.T) {
	for _, tc := range []struct {
		name     string
		keyBits  uint
		existing int
		batch    int
		// overlap is how many of the batch keys already exist, exercising the
		// replace path next to the insert path.
		overlap int
	}{
		{name: "into-empty", keyBits: 32, existing: 0, batch: 64},
		{name: "single-key", keyBits: 32, existing: 128, batch: 1},
		{name: "all-inserts", keyBits: 32, existing: 512, batch: 128},
		{name: "all-replaces", keyBits: 32, existing: 512, batch: 128, overlap: 128},
		{name: "mixed", keyBits: 32, existing: 512, batch: 128, overlap: 64},
		{name: "narrow-keys", keyBits: 8, existing: 100, batch: 60, overlap: 30},
		{name: "wide-keys", keyBits: 256, existing: 400, batch: 200, overlap: 50},
		{name: "dense-small", keyBits: 4, existing: 8, batch: 8, overlap: 4},
		{name: "batch-larger-than-dict", keyBits: 16, existing: 16, batch: 256, overlap: 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rnd := rand.New(rand.NewSource(int64(len(tc.name)) + 2026080901))
			existing := randomBulkKeys(t, rnd, tc.keyBits, tc.existing, nil)

			seed := func() *AugmentedDictionary {
				dict, err := NewAugDict(tc.keyBits, bulkSumAugmentation{})
				if err != nil {
					t.Fatal(err)
				}
				for _, key := range existing {
					if err := dict.Set(key, bulkValue(key, 0)); err != nil {
						t.Fatal(err)
					}
				}
				return dict
			}

			batch := make([]AugmentedEntry, 0, tc.batch)
			for i := 0; i < tc.overlap && i < len(existing); i++ {
				batch = append(batch, AugmentedEntry{Key: existing[i], Value: bulkValue(existing[i], 1)})
			}
			fresh := randomBulkKeys(t, rnd, tc.keyBits, tc.batch-len(batch), existing)
			for _, key := range fresh {
				batch = append(batch, AugmentedEntry{Key: key, Value: bulkValue(key, 2)})
			}
			// Deliberately unsorted: SetMany owns the ordering.
			rnd.Shuffle(len(batch), func(i, j int) { batch[i], batch[j] = batch[j], batch[i] })

			oneByOne := seed()
			for _, entry := range batch {
				if err := oneByOne.Set(entry.Key, entry.Value); err != nil {
					t.Fatalf("set %x: %v", entry.Key.Hash()[:4], err)
				}
			}
			bulk := seed()
			if err := bulk.SetMany(batch); err != nil {
				t.Fatalf("SetMany: %v", err)
			}

			want, err := oneByOne.ToCell()
			if err != nil {
				t.Fatal(err)
			}
			got, err := bulk.ToCell()
			if err != nil {
				t.Fatal(err)
			}
			if got.HashKey() != want.HashKey() {
				t.Fatalf("bulk root = %x, repeated Set = %x", got.Hash()[:8], want.Hash()[:8])
			}

			// A matching root already implies matching values, but reading them
			// back proves the tree is navigable rather than merely equal.
			for _, entry := range batch {
				value, err := bulk.LoadValue(entry.Key)
				if err != nil {
					t.Fatalf("read back %x: %v", entry.Key.Hash()[:4], err)
				}
				if value.MustToCell().HashKey() != entry.Value.HashKey() {
					t.Fatalf("read back %x gave the wrong value", entry.Key.Hash()[:4])
				}
			}
		})
	}
}

// TestSetManyMatchesRepeatedSetRandomized sweeps shapes the table above fixes,
// because the divergence cases depend on where keys happen to branch.
func TestSetManyMatchesRepeatedSetRandomized(t *testing.T) {
	rnd := rand.New(rand.NewSource(2026080902))
	for round := 0; round < 300; round++ {
		keyBits := uint(1 + rnd.Intn(24))
		maxKeys := 1 << min(keyBits, 10)
		existingCount := rnd.Intn(min(40, maxKeys) + 1)
		batchCount := 1 + rnd.Intn(min(40, maxKeys))

		existing := randomBulkKeys(t, rnd, keyBits, existingCount, nil)
		seed := func() *AugmentedDictionary {
			dict, err := NewAugDict(keyBits, bulkSumAugmentation{})
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range existing {
				if err := dict.Set(key, bulkValue(key, 0)); err != nil {
					t.Fatal(err)
				}
			}
			return dict
		}

		var batch []AugmentedEntry
		used := map[string]struct{}{}
		for len(batch) < batchCount {
			var key *Cell
			if len(existing) > 0 && rnd.Intn(2) == 0 {
				key = existing[rnd.Intn(len(existing))]
			} else {
				key = randomBulkKey(t, rnd, keyBits)
			}
			id := string(key.Hash())
			if _, seen := used[id]; seen {
				continue
			}
			used[id] = struct{}{}
			batch = append(batch, AugmentedEntry{Key: key, Value: bulkValue(key, uint64(round))})
		}

		oneByOne := seed()
		for _, entry := range batch {
			if err := oneByOne.Set(entry.Key, entry.Value); err != nil {
				t.Fatalf("round %d set: %v", round, err)
			}
		}
		bulk := seed()
		if err := bulk.SetMany(batch); err != nil {
			t.Fatalf("round %d SetMany: %v", round, err)
		}

		want, err := oneByOne.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		got, err := bulk.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		if got.HashKey() != want.HashKey() {
			t.Fatalf("round %d (keyBits=%d existing=%d batch=%d): bulk %x != repeated %x",
				round, keyBits, existingCount, batchCount, got.Hash()[:8], want.Hash()[:8])
		}
	}
}

func TestSetManyRejectsBadInput(t *testing.T) {
	dict, err := NewAugDict(32, bulkSumAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	key := BeginCell().MustStoreUInt(1, 32).EndCell()

	if err := dict.SetMany(nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if err := dict.SetMany([]AugmentedEntry{{Key: key, Value: nil}}); err == nil {
		t.Fatal("nil value accepted")
	}
	if err := dict.SetMany([]AugmentedEntry{{Key: BeginCell().MustStoreUInt(1, 16).EndCell(), Value: key}}); err == nil {
		t.Fatal("short key accepted")
	}
	if err := dict.SetMany([]AugmentedEntry{{Key: key, Value: key}, {Key: key, Value: key}}); err == nil {
		t.Fatal("duplicate key accepted")
	}

	// Add and Replace are assertions: a bulk write reports no per-entry result,
	// so a caller relying on them to catch bookkeeping mistakes needs a failure.
	if err := dict.SetMany([]AugmentedEntry{{Key: key, Value: key, Mode: DictSetModeReplace}}); err == nil {
		t.Fatal("replace of an absent key accepted")
	}
	if err := dict.SetMany([]AugmentedEntry{{Key: key, Value: key, Mode: DictSetModeAdd}}); err != nil {
		t.Fatalf("add of an absent key: %v", err)
	}
	if err := dict.SetMany([]AugmentedEntry{{Key: key, Value: key, Mode: DictSetModeAdd}}); err == nil {
		t.Fatal("add of an existing key accepted")
	}
	if err := dict.SetMany([]AugmentedEntry{{Key: key, Value: key, Mode: DictSetModeReplace}}); err != nil {
		t.Fatalf("replace of an existing key: %v", err)
	}

	// Deep placements must be checked too, not only the root leaf.
	deep := make([]AugmentedEntry, 0, 32)
	for i := range 32 {
		deep = append(deep, AugmentedEntry{
			Key:   BeginCell().MustStoreUInt(uint64(i)<<8|0xAB, 32).EndCell(),
			Value: key,
			Mode:  DictSetModeAdd,
		})
	}
	if err := dict.SetMany(deep); err != nil {
		t.Fatalf("deep add batch: %v", err)
	}
	deep[17].Mode = DictSetModeAdd
	if err := dict.SetMany(deep); err == nil {
		t.Fatal("add of existing keys deep in the tree accepted")
	}
	for i := range deep {
		deep[i].Mode = DictSetModeReplace
	}
	if err := dict.SetMany(deep); err != nil {
		t.Fatalf("deep replace batch: %v", err)
	}
}

// Bulk writes identify a key by the bytes behind it, so a key size that leaves
// a partial trailing byte must not let that byte's padding decide the answer.
// The padding is not canonical: EndCell zero-fills it while a BoC round trip
// leaves the completion tag in place, so the same key reaching SetMany from the
// two sources used to sort apart and defeat the distinctness check — the batch
// then either failed deep in the descent or silently dropped an entry.
func TestBulkKeyComparisonIgnoresPadding(t *testing.T) {
	for _, keyBits := range []uint{4, 12, 17, 31} {
		t.Run(fmt.Sprintf("bits=%d", keyBits), func(t *testing.T) {
			built := BeginCell().MustStoreUInt(0xABCDEF&((1<<keyBits)-1), keyBits).EndCell()
			parsed, err := FromBOC(built.ToBOC())
			if err != nil {
				t.Fatal(err)
			}
			if built.HashKey() != parsed.HashKey() {
				t.Fatalf("fixture broken: the two cells are not the same key")
			}

			var a, b Slice
			if err = built.BeginParseInto(&a); err != nil {
				t.Fatal(err)
			}
			if err = parsed.BeginParseInto(&b); err != nil {
				t.Fatal(err)
			}
			if got := compareKeySlices(&a, &b); got != 0 {
				t.Fatalf("compareKeySlices of the same key from two sources = %d, want 0", got)
			}

			dict, err := NewAugDict(keyBits, bulkSumAugmentation{})
			if err != nil {
				t.Fatal(err)
			}
			err = dict.SetMany([]AugmentedEntry{
				{Key: built, Value: bulkValue(built, 1)},
				{Key: parsed, Value: bulkValue(parsed, 2)},
			})
			if err == nil {
				t.Fatal("SetMany accepted the same key twice")
			}
			if !strings.Contains(err.Error(), "duplicate key") {
				t.Fatalf("SetMany error = %v, want a duplicate-key rejection", err)
			}
		})
	}
}

// Ordering must still follow the key bits themselves, so masking the trailing
// byte may not flatten keys that genuinely differ inside it.
func TestBulkKeyComparisonOrdersByKeyBits(t *testing.T) {
	const keyBits = 12
	lower := BeginCell().MustStoreUInt(0xABC, keyBits).EndCell()
	higher := BeginCell().MustStoreUInt(0xABD, keyBits).EndCell()

	var a, b Slice
	if err := lower.BeginParseInto(&a); err != nil {
		t.Fatal(err)
	}
	if err := higher.BeginParseInto(&b); err != nil {
		t.Fatal(err)
	}
	if got := compareKeySlices(&a, &b); got >= 0 {
		t.Fatalf("compareKeySlices(0xABC, 0xABD) = %d, want < 0", got)
	}
	if got := compareKeySlices(&b, &a); got <= 0 {
		t.Fatalf("compareKeySlices(0xABD, 0xABC) = %d, want > 0", got)
	}
}

// splitBulkOnNextBit is the single partitioning step of both bulk descents, and
// each descent instantiates it over its own item type. The whole batch depends
// on it consuming exactly one bit per item and cutting the sorted slice in
// place, so both instantiations are pinned here rather than only through the
// end-to-end comparisons above.
func TestSplitBulkOnNextBit(t *testing.T) {
	// 3-bit keys in sorted order: the edge bit turns to 1 at index 4.
	keys := []uint64{0b000, 0b001, 0b010, 0b011, 0b100, 0b101}

	for _, tc := range []struct {
		name  string
		take  int
		split int
	}{
		{name: "both-sides", take: 6, split: 4},
		{name: "zero-side-only", take: 4, split: 4},
		{name: "one-side-only", take: 2, split: 0},
		{name: "single-item", take: 1, split: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from := 0
			if tc.split == 0 {
				from = 4
			}
			batch := keys[from : from+tc.take]

			slices := make([]Slice, len(batch))
			items := make([]augBulkItem, len(batch))
			for i, key := range batch {
				if err := BeginCell().MustStoreUInt(key, 3).EndCell().BeginParseInto(&slices[i]); err != nil {
					t.Fatal(err)
				}
				items[i].key = slices[i]
			}

			left, right, err := splitBulkOnNextBit(slices, func(key *Slice) *Slice { return key })
			if err != nil {
				t.Fatalf("split slices: %v", err)
			}
			itemsLeft, itemsRight, err := splitBulkOnNextBit(items, augBulkItemKey)
			if err != nil {
				t.Fatalf("split items: %v", err)
			}
			if len(left) != tc.split || len(itemsLeft) != tc.split {
				t.Fatalf("zero side = %d slices / %d items, want %d", len(left), len(itemsLeft), tc.split)
			}
			if len(right) != len(batch)-tc.split || len(itemsRight) != len(batch)-tc.split {
				t.Fatalf("one side = %d slices / %d items, want %d", len(right), len(itemsRight), len(batch)-tc.split)
			}
			if len(left) > 0 && &left[0] != &slices[0] {
				t.Fatal("zero side was copied instead of reslicing the batch")
			}
			if len(right) > 0 && &right[0] != &slices[tc.split] {
				t.Fatal("one side was copied instead of reslicing the batch")
			}

			for i, key := range batch {
				if slices[i].BitsLeft() != 2 || items[i].key.BitsLeft() != 2 {
					t.Fatalf("item %d has %d bits left, want 2", i, slices[i].BitsLeft())
				}
				suffix := slices[i].MustLoadUInt(2)
				if suffix != key&0b11 {
					t.Fatalf("item %d suffix = %03b, want %03b", i, suffix, key&0b11)
				}
				edge := uint64(0)
				if i >= tc.split {
					edge = 1
				}
				if key>>2 != edge {
					t.Fatalf("item %d landed on the %d side but its edge bit is %d", i, edge, key>>2)
				}
			}
		})
	}

	// A batch that reached the end of its keys cannot be split: the callers rely
	// on that failing rather than silently partitioning on nothing.
	spent := make([]Slice, 1)
	if err := BeginCell().EndCell().BeginParseInto(&spent[0]); err != nil {
		t.Fatal(err)
	}
	if _, _, err := splitBulkOnNextBit(spent, func(key *Slice) *Slice { return key }); err == nil {
		t.Fatal("split of an exhausted key accepted")
	}
}

// bulkSumAugmentation sums a per-leaf metric in 64 bits. The package's 16-bit
// test augmentation wraps once a few hundred leaves are in play, which would
// mask real differences behind a shared overflow.
type bulkSumAugmentation struct{}

func (bulkSumAugmentation) SkipExtra(loader *Slice) error {
	_, err := loader.LoadUInt(64)
	return err
}

func (bulkSumAugmentation) EmptyExtra(dst *Builder) error {
	return dst.StoreUInt(0, 64)
}

func (bulkSumAugmentation) LeafExtra(value *Slice, dst *Builder) error {
	return dst.StoreUInt(uint64(value.BitsLeft())+uint64(value.RefsNum())*257, 64)
}

func (bulkSumAugmentation) CombineExtra(leftExtra, rightExtra *Slice, dst *Builder) error {
	left, err := leftExtra.LoadUInt(64)
	if err != nil {
		return err
	}
	right, err := rightExtra.LoadUInt(64)
	if err != nil {
		return err
	}
	return dst.StoreUInt(left+right, 64)
}

func randomBulkKeys(tb testing.TB, rnd *rand.Rand, keyBits uint, count int, avoid []*Cell) []*Cell {
	tb.Helper()

	used := make(map[string]struct{}, count+len(avoid))
	for _, key := range avoid {
		used[string(key.Hash())] = struct{}{}
	}
	keys := make([]*Cell, 0, count)
	for len(keys) < count {
		key := randomBulkKey(tb, rnd, keyBits)
		id := string(key.Hash())
		if _, seen := used[id]; seen {
			continue
		}
		used[id] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func randomBulkKey(tb testing.TB, rnd *rand.Rand, keyBits uint) *Cell {
	tb.Helper()

	b := BeginCell()
	for left := keyBits; left > 0; {
		chunk := min(left, 32)
		if err := b.StoreUInt(rnd.Uint64()&(1<<chunk-1), chunk); err != nil {
			tb.Fatal(err)
		}
		left -= chunk
	}
	return b.EndCell()
}

// bulkValue gives every (key, generation) pair its own cell with a reference, so
// the augmentation actually varies across the tree.
func bulkValue(key *Cell, generation uint64) *Cell {
	return BeginCell().
		MustStoreUInt(generation, 16).
		MustStoreRef(BeginCell().MustStoreStringSnake(fmt.Sprintf("v%x-%d", key.Hash()[:4], generation)).EndCell()).
		EndCell()
}

func BenchmarkAugDictSetManyVersusRepeatedSet(b *testing.B) {
	const keyBits = 256
	rnd := rand.New(rand.NewSource(2026080903))
	existing := randomBulkKeys(b, rnd, keyBits, 100_000, nil)

	base, err := NewAugDict(keyBits, bulkSumAugmentation{})
	if err != nil {
		b.Fatal(err)
	}
	for _, key := range existing {
		if err := base.Set(key, bulkValue(key, 0)); err != nil {
			b.Fatal(err)
		}
	}

	for _, size := range []int{100, 1000} {
		batch := make([]AugmentedEntry, size)
		for i := range batch {
			key := existing[rnd.Intn(len(existing))]
			batch[i] = AugmentedEntry{Key: key, Value: bulkValue(key, uint64(i+1))}
		}
		// Distinct keys only.
		seen := map[string]struct{}{}
		unique := batch[:0]
		for _, entry := range batch {
			if _, ok := seen[string(entry.Key.Hash())]; ok {
				continue
			}
			seen[string(entry.Key.Hash())] = struct{}{}
			unique = append(unique, entry)
		}
		batch = unique

		b.Run(fmt.Sprintf("keys=%d/repeated", len(batch)), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				dict := base.Copy()
				for _, entry := range batch {
					if err := dict.Set(entry.Key, entry.Value); err != nil {
						b.Fatal(err)
					}
				}
				if _, err := dict.ToCell(); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("keys=%d/bulk", len(batch)), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				dict := base.Copy()
				if err := dict.SetMany(batch); err != nil {
					b.Fatal(err)
				}
				if _, err := dict.ToCell(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
