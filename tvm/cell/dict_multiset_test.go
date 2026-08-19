package cell

import (
	"bytes"
	"errors"
	"math/rand"
	"testing"
)

func multisetTestKey(rnd *rand.Rand, keySz uint, shared []byte) []byte {
	key := make([]byte, (keySz+7)/8)
	rnd.Read(key)
	// Reuse a shared head often, so the tree grows deep shared labels instead
	// of splitting at the root and never exercising the label cases.
	if len(shared) > 0 && rnd.Intn(2) == 0 {
		copy(key, shared[:1+rnd.Intn(len(shared))])
	}
	if tail := keySz % 8; tail != 0 {
		key[len(key)-1] &= ^byte(0) << (8 - tail)
	}
	return key
}

func multisetTestValue(rnd *rand.Rand) *Builder {
	return BeginCell().MustStoreUInt(rnd.Uint64()&(1<<34-1), 34)
}

// applySequentially is the ground truth: the same batch as one Set or Delete
// per key, which is what the storage-stat commit loop did before Multiset.
func applySequentially(t *testing.T, dict *Dictionary, batch []DictBulkKV) error {
	t.Helper()
	for _, item := range batch {
		if item.Value == nil {
			if err := dict.DeleteByBytesKey(item.Key); err != nil {
				return err
			}
			continue
		}
		if err := dict.SetBuilderByBytesKey(item.Key, item.Value); err != nil {
			return err
		}
	}
	return nil
}

func dictRootHash(d *Dictionary) []byte {
	root := d.AsCell()
	if root == nil {
		return nil
	}
	return root.Hash()
}

// TestMultisetMatchesSequentialUpdates is the correctness gate: over many
// random tree shapes and batches — inserts, overwrites, deletes and deletes
// that empty whole subtrees — the batch pass must produce the byte-identical
// dictionary a Set/Delete loop produces. Dictionary serialization is
// canonical, so equal content means an equal root hash.
func TestMultisetMatchesSequentialUpdates(t *testing.T) {
	rnd := rand.New(rand.NewSource(20260819))

	for _, keySz := range []uint{8, 32, 256} {
		for round := 0; round < 120; round++ {
			shared := make([]byte, (keySz+7)/8)
			rnd.Read(shared)

			initial := make([]DictBulkKV, 0, 24)
			present := make([][]byte, 0, 24)
			for i, n := 0, rnd.Intn(24); i < n; i++ {
				key := multisetTestKey(rnd, keySz, shared)
				initial = append(initial, DictBulkKV{Key: key, Value: multisetTestValue(rnd)})
				present = append(present, key)
			}

			base := NewDict(keySz)
			if err := applySequentially(t, base, initial); err != nil {
				t.Fatalf("keySz=%d round=%d: seed the tree: %v", keySz, round, err)
			}

			batch := make([]DictBulkKV, 0, 12)
			used := make(map[string]struct{}, 12)
			for i, n := 0, 1+rnd.Intn(12); i < n; i++ {
				var key []byte
				// Half the batch hits existing keys, so overwrites and real
				// deletes both occur; the rest are fresh inserts.
				if len(present) > 0 && rnd.Intn(2) == 0 {
					key = present[rnd.Intn(len(present))]
				} else {
					key = multisetTestKey(rnd, keySz, shared)
				}
				if _, duplicate := used[string(key)]; duplicate {
					continue
				}
				used[string(key)] = struct{}{}

				// A delete is only legal for a key the tree holds; anything
				// else carries a value.
				value := multisetTestValue(rnd)
				if _, err := base.LoadValueByBytesKey(key); err == nil && rnd.Intn(3) == 0 {
					value = nil
				}
				batch = append(batch, DictBulkKV{Key: key, Value: value})
			}

			sequential := base.Copy()
			batched := base.Copy()

			seqErr := applySequentially(t, sequential, append([]DictBulkKV(nil), batch...))
			batchErr := batched.Multiset(append([]DictBulkKV(nil), batch...))
			if seqErr != nil || batchErr != nil {
				t.Fatalf("keySz=%d round=%d: sequential err=%v, batch err=%v", keySz, round, seqErr, batchErr)
			}

			if !bytes.Equal(dictRootHash(sequential), dictRootHash(batched)) {
				t.Fatalf("keySz=%d round=%d: batch root %x differs from sequential %x (initial=%d, batch=%d)",
					keySz, round, dictRootHash(batched), dictRootHash(sequential), len(initial), len(batch))
			}

			// The content has to agree key by key, not just by root hash, so a
			// shape that serializes equally but reads differently still fails.
			for _, item := range batch {
				got, gotErr := batched.LoadValueByBytesKey(item.Key)
				want, wantErr := sequential.LoadValueByBytesKey(item.Key)
				if (gotErr == nil) != (wantErr == nil) {
					t.Fatalf("keySz=%d round=%d key %x: batch err=%v, sequential err=%v",
						keySz, round, item.Key, gotErr, wantErr)
				}
				if gotErr == nil && !bytes.Equal(got.MustToCell().Hash(), want.MustToCell().Hash()) {
					t.Fatalf("keySz=%d round=%d key %x: values differ", keySz, round, item.Key)
				}
			}
		}
	}
}

// TestMultisetDeleteEmptyingTheTree covers the shapes the random rounds reach
// rarely: every key deleted, the root collapsing to a single leaf, and a
// delete of a key the dictionary does not hold.
func TestMultisetDeleteEmptyingTheTree(t *testing.T) {
	keys := [][]byte{
		{0b0000_0000}, {0b0100_0000}, {0b1000_0000}, {0b1100_0000},
	}
	build := func(t *testing.T) *Dictionary {
		t.Helper()
		d := NewDict(8)
		for i, key := range keys {
			if err := d.SetBuilderByBytesKey(key, BeginCell().MustStoreUInt(uint64(i+1), 34)); err != nil {
				t.Fatalf("seed %x: %v", key, err)
			}
		}
		return d
	}

	t.Run("delete all", func(t *testing.T) {
		d := build(t)
		batch := make([]DictBulkKV, 0, len(keys))
		for _, key := range keys {
			batch = append(batch, DictBulkKV{Key: key})
		}
		if err := d.Multiset(batch); err != nil {
			t.Fatalf("delete all: %v", err)
		}
		if root := d.AsCell(); root != nil {
			t.Fatalf("emptied dictionary kept a root %x", root.Hash())
		}
	})

	t.Run("collapse to one leaf", func(t *testing.T) {
		d, want := build(t), build(t)
		batch := []DictBulkKV{{Key: keys[0]}, {Key: keys[1]}, {Key: keys[2]}}
		if err := d.Multiset(batch); err != nil {
			t.Fatalf("collapse: %v", err)
		}
		if err := applySequentially(t, want, batch); err != nil {
			t.Fatalf("collapse ground truth: %v", err)
		}
		if !bytes.Equal(dictRootHash(d), dictRootHash(want)) {
			t.Fatalf("collapsed root %x, want %x", dictRootHash(d), dictRootHash(want))
		}
		if _, err := d.LoadValueByBytesKey(keys[3]); err != nil {
			t.Fatalf("surviving key unreadable: %v", err)
		}
	})

	t.Run("delete absent key", func(t *testing.T) {
		d := build(t)
		before := dictRootHash(d)
		err := d.Multiset([]DictBulkKV{{Key: []byte{0b0010_0000}}})
		if !errors.Is(err, ErrNoSuchKeyInDict) {
			t.Fatalf("delete of an absent key: got %v, want ErrNoSuchKeyInDict", err)
		}
		if !bytes.Equal(dictRootHash(d), before) {
			t.Fatal("failed batch moved the root")
		}
	})

	t.Run("duplicate key", func(t *testing.T) {
		d := build(t)
		err := d.Multiset([]DictBulkKV{
			{Key: keys[0], Value: BeginCell().MustStoreUInt(7, 34)},
			{Key: keys[0], Value: BeginCell().MustStoreUInt(8, 34)},
		})
		if err == nil {
			t.Fatal("a batch naming one key twice was accepted")
		}
	})
}

// TestMultisetLeavesUntouchedSubtreesUnread is the proof-shape property the
// reference relies on and the reason the batch form is not merely faster: a
// subtree no key of the batch descends into is carried over by reference, so
// it never enters the read set and prunes away in a Merkle proof. A validator
// replaying the same batch therefore needs exactly what the producer read.
func TestMultisetLeavesUntouchedSubtreesUnread(t *testing.T) {
	// Two keys under the 0-side and two under the 1-side, so the sides are
	// whole subtrees rather than single leaves.
	keys := [][]byte{
		{0b0000_0000}, {0b0010_0000}, {0b1000_0000}, {0b1010_0000},
	}
	source := NewDict(8)
	for i, key := range keys {
		if err := source.SetBuilderByBytesKey(key, BeginCell().MustStoreUInt(uint64(i+1), 34)); err != nil {
			t.Fatalf("seed %x: %v", key, err)
		}
	}
	root := source.AsCell()

	// The untouched side's subtree root, to check it against the read set.
	untouched := root.MustPeekRef(1)

	read := NewReadSet(root)
	dict := read.Root().AsDict(8)
	if err := dict.Multiset([]DictBulkKV{
		{Key: keys[0], Value: BeginCell().MustStoreUInt(99, 34)},
	}); err != nil {
		t.Fatalf("batch update: %v", err)
	}

	if _, recorded := read.Contains(untouched.HashKey()); recorded {
		t.Fatal("the batch read a subtree no key of it descends into")
	}

	proof, err := read.Proof()
	if err != nil {
		t.Fatalf("build proof: %v", err)
	}
	body, err := UnwrapProofVirtualized(proof, root.Hash())
	if err != nil {
		t.Fatalf("virtualize proof: %v", err)
	}
	if kept := body.MustPeekRef(1); kept.GetType() != PrunedCellType {
		t.Fatal("the untouched subtree survived into the proof, so the batch did read it")
	}

	// The same update run sequentially is free to read more; what must hold is
	// that the batch's own proof carries its own replay.
	replay, err := UnwrapProofVirtualized(proof, root.Hash())
	if err != nil {
		t.Fatalf("virtualize replay proof: %v", err)
	}
	replayed := replay.AsDict(8)
	if err = replayed.Multiset([]DictBulkKV{
		{Key: keys[0], Value: BeginCell().MustStoreUInt(99, 34)},
	}); err != nil {
		t.Fatalf("replay of the batch on its own proof: %v", err)
	}
	if !bytes.Equal(dictRootHash(replayed), dictRootHash(dict)) {
		t.Fatalf("replay produced root %x, want %x", dictRootHash(replayed), dictRootHash(dict))
	}
}

// TestMultisetRejectsPrunedNodesOnItsPath is the negative half: where the
// batch does have to descend, a pruned node must be reported as one and never
// parsed as a label, the same rule the descent and the delete-merge follow.
func TestMultisetRejectsPrunedNodesOnItsPath(t *testing.T) {
	root, keys := buildPrunedSiblingDict(t)
	dict := virtualizedDictProof(t, root, keys[0])
	before := dictRootHash(dict)

	err := dict.Multiset([]DictBulkKV{
		{Key: keys[1], Value: BeginCell().MustStoreUInt(5, 34)},
	})
	if !errors.Is(err, ErrDictHasSpecialCells) {
		t.Fatalf("batch into a pruned subtree: got %v, want ErrDictHasSpecialCells", err)
	}
	if !bytes.Equal(dictRootHash(dict), before) {
		t.Fatal("a failed batch moved the root")
	}
}

func BenchmarkDictMultisetVsSequential(b *testing.B) {
	rnd := rand.New(rand.NewSource(1))
	const keySz = 256
	base := NewDict(keySz)
	for i := 0; i < 512; i++ {
		key := multisetTestKey(rnd, keySz, nil)
		if err := base.SetBuilderByBytesKey(key, multisetTestValue(rnd)); err != nil {
			b.Fatal(err)
		}
	}
	batch := make([]DictBulkKV, 0, 16)
	for i := 0; i < 16; i++ {
		batch = append(batch, DictBulkKV{Key: multisetTestKey(rnd, keySz, nil), Value: multisetTestValue(rnd)})
	}

	// The two arms both copy the dictionary handle first, so the copy is
	// measured on its own and subtracted by the reader rather than credited to
	// either form.
	b.Run("copy only", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = base.Copy()
		}
	})
	b.Run("multiset", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			dict := base.Copy()
			items := append([]DictBulkKV(nil), batch...)
			if err := dict.Multiset(items); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("sequential", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			dict := base.Copy()
			for _, item := range batch {
				if err := dict.SetBuilderByBytesKey(item.Key, item.Value); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
