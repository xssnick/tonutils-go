package cell

import (
	"bytes"
	"encoding/hex"
	"math/rand"
	"testing"
)

func bulkTestKey(rnd *rand.Rand, keySz uint, base []byte) []byte {
	keyBytes := int(keySz+7) / 8
	key := make([]byte, keyBytes)
	if base != nil && rnd.Intn(2) == 0 {
		// cluster around a shared base to force deep common prefixes
		copy(key, base)
		key[keyBytes-1] = byte(rnd.Intn(256))
		if keyBytes > 1 && rnd.Intn(2) == 0 {
			key[keyBytes-2] = byte(rnd.Intn(4))
		}
	} else {
		rnd.Read(key)
	}
	return key
}

func bulkTestValue(rnd *rand.Rand) *Builder {
	sz := uint(1 + rnd.Intn(60))
	b := BeginCell().MustStoreUInt(rnd.Uint64()&(1<<sz-1), sz)
	if rnd.Intn(8) == 0 {
		b.MustStoreRef(BeginCell().MustStoreUInt(uint64(rnd.Uint32()), 32).EndCell())
	}
	return b
}

func bulkKeyCell(t *testing.T, key []byte, keySz uint) *Cell {
	t.Helper()
	return BeginCell().MustStoreSlice(key, keySz).EndCell()
}

func compareDictRoots(t *testing.T, want, got *Dictionary, context string) {
	t.Helper()

	wantRoot, gotRoot := want.AsCell(), got.AsCell()
	if (wantRoot == nil) != (gotRoot == nil) {
		t.Fatalf("%s: nil root mismatch: want nil=%v, got nil=%v", context, wantRoot == nil, gotRoot == nil)
	}
	if wantRoot == nil {
		return
	}
	if !bytes.Equal(wantRoot.Hash(), gotRoot.Hash()) {
		t.Fatalf("%s: root hash mismatch: want %x, got %x", context, wantRoot.Hash(), gotRoot.Hash())
	}
}

func TestNewDictFromItemsEquivalence(t *testing.T) {
	rnd := rand.New(rand.NewSource(42))

	for _, keySz := range []uint{8, 32, 256} {
		for _, count := range []int{0, 1, 2, 3, 7, 32, 200} {
			for iter := 0; iter < 6; iter++ {
				keyBytes := int(keySz+7) / 8
				base := make([]byte, keyBytes)
				rnd.Read(base)

				seen := map[string]bool{}
				items := make([]DictBulkKV, 0, count)
				for len(items) < count {
					key := bulkTestKey(rnd, keySz, base)
					if seen[hex.EncodeToString(key)] {
						continue
					}
					seen[hex.EncodeToString(key)] = true
					items = append(items, DictBulkKV{Key: key, Value: bulkTestValue(rnd)})
				}

				incremental := NewDict(keySz)
				for _, kv := range items {
					if err := incremental.SetBuilder(bulkKeyCell(t, kv.Key, keySz), kv.Value); err != nil {
						t.Fatalf("keySz=%d count=%d: incremental set: %v", keySz, count, err)
					}
				}

				// bulk build takes the items in an independent random order
				shuffled := make([]DictBulkKV, len(items))
				copy(shuffled, items)
				rnd.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

				bulk, err := NewDictFromItems(keySz, shuffled)
				if err != nil {
					t.Fatalf("keySz=%d count=%d: bulk build: %v", keySz, count, err)
				}
				compareDictRoots(t, incremental, bulk, "bulk vs incremental")

				for _, kv := range items {
					got, err := bulk.LoadValueByBytesKey(kv.Key)
					if err != nil {
						t.Fatalf("keySz=%d count=%d: bulk lookup %x: %v", keySz, count, kv.Key, err)
					}
					want := kv.Value.EndCell()
					if !bytes.Equal(got.MustToCell().Hash(), want.Hash()) {
						t.Fatalf("keySz=%d count=%d: bulk value mismatch for key %x", keySz, count, kv.Key)
					}
				}
			}
		}
	}
}

func TestNewDictFromItemsEdgeCases(t *testing.T) {
	value := func(v uint64) *Builder { return BeginCell().MustStoreUInt(v, 34) }

	buildBoth := func(t *testing.T, keySz uint, items []DictBulkKV) (*Dictionary, *Dictionary) {
		t.Helper()

		incremental := NewDict(keySz)
		for _, kv := range items {
			if err := incremental.SetBuilder(bulkKeyCell(t, kv.Key, keySz), kv.Value); err != nil {
				t.Fatal(err)
			}
		}
		bulk, err := NewDictFromItems(keySz, items)
		if err != nil {
			t.Fatal(err)
		}
		compareDictRoots(t, incremental, bulk, "bulk vs incremental")
		return incremental, bulk
	}

	t.Run("empty", func(t *testing.T) {
		bulk, err := NewDictFromItems(256, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bulk.IsEmpty() || bulk.AsCell() != nil {
			t.Fatal("expected empty dict with nil root")
		}
	})

	t.Run("single entry", func(t *testing.T) {
		key := bytes.Repeat([]byte{0xAB}, 32)
		buildBoth(t, 256, []DictBulkKV{{Key: key, Value: value(7)}})
	})

	t.Run("two keys differing only in last bit", func(t *testing.T) {
		a := bytes.Repeat([]byte{0x55}, 32)
		b := bytes.Repeat([]byte{0x55}, 32)
		b[31] ^= 0x01
		buildBoth(t, 256, []DictBulkKV{
			{Key: a, Value: value(1)},
			{Key: b, Value: value(2)},
		})
	})

	t.Run("hml_same labels", func(t *testing.T) {
		zeros := make([]byte, 32)
		ones := bytes.Repeat([]byte{0xFF}, 32)
		lowBit := make([]byte, 32)
		lowBit[31] = 0x01
		highBit := make([]byte, 32)
		highBit[0] = 0x80
		almostOnes := bytes.Repeat([]byte{0xFF}, 32)
		almostOnes[31] = 0xFE
		buildBoth(t, 256, []DictBulkKV{
			{Key: zeros, Value: value(1)},
			{Key: ones, Value: value(2)},
			{Key: lowBit, Value: value(3)},
			{Key: highBit, Value: value(4)},
			{Key: almostOnes, Value: value(5)},
		})
	})

	t.Run("duplicate keys last wins", func(t *testing.T) {
		key := bytes.Repeat([]byte{0x11}, 32)
		other := bytes.Repeat([]byte{0x22}, 32)
		items := []DictBulkKV{
			{Key: key, Value: value(1)},
			{Key: other, Value: value(2)},
			{Key: key, Value: value(3)},
		}
		_, bulk := buildBoth(t, 256, items)

		got, err := bulk.LoadValueByBytesKey(key)
		if err != nil {
			t.Fatal(err)
		}
		if v := got.MustLoadUInt(34); v != 3 {
			t.Fatalf("expected last duplicate to win, got value %d", v)
		}
	})

	t.Run("non byte aligned key size ignores trailing bits", func(t *testing.T) {
		// keys carry garbage beyond the 13th bit which must be ignored
		items := []DictBulkKV{
			{Key: []byte{0x00, 0x07}, Value: value(1)},
			{Key: []byte{0x00, 0x0F}, Value: value(2)}, // same 13 key bits as above
			{Key: []byte{0x80, 0x01}, Value: value(3)},
			{Key: []byte{0x80, 0x0A}, Value: value(4)}, // same 13 key bits as above
			{Key: []byte{0xFF, 0xFF}, Value: value(5)},
		}
		bulk, err := NewDictFromItems(13, items)
		if err != nil {
			t.Fatal(err)
		}

		incremental := NewDict(13)
		for _, kv := range items {
			if err := incremental.SetBuilder(bulkKeyCell(t, kv.Key, 13), kv.Value); err != nil {
				t.Fatal(err)
			}
		}
		compareDictRoots(t, incremental, bulk, "keySz=13")
	})

	t.Run("short key rejected", func(t *testing.T) {
		if _, err := NewDictFromItems(256, []DictBulkKV{{Key: make([]byte, 31), Value: value(1)}}); err == nil {
			t.Fatal("expected short key error")
		}
	})

	t.Run("nil value rejected", func(t *testing.T) {
		if _, err := NewDictFromItems(256, []DictBulkKV{{Key: make([]byte, 32)}}); err == nil {
			t.Fatal("expected nil value error")
		}
	})

	t.Run("oversized key size rejected", func(t *testing.T) {
		if _, err := NewDictFromItems(1024, nil); err == nil {
			t.Fatal("expected key size error")
		}
	})
}

func TestDictionaryBytesKeyOps(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))

	for _, keySz := range []uint{32, 256} {
		reference := NewDict(keySz)
		byBytes := NewDict(keySz)

		keys := make([][]byte, 0, 64)
		for i := 0; i < 64; i++ {
			key := bulkTestKey(rnd, keySz, nil)
			keys = append(keys, key)
			value := bulkTestValue(rnd)

			if err := reference.SetBuilder(bulkKeyCell(t, key, keySz), value); err != nil {
				t.Fatal(err)
			}
			if err := byBytes.SetBuilderByBytesKey(key, value); err != nil {
				t.Fatal(err)
			}
			compareDictRoots(t, reference, byBytes, "after set")
		}

		for _, key := range keys {
			want, err := reference.LoadValue(bulkKeyCell(t, key, keySz))
			if err != nil {
				t.Fatal(err)
			}
			got, err := byBytes.LoadValueByBytesKey(key)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want.MustToCell().Hash(), got.MustToCell().Hash()) {
				t.Fatalf("keySz=%d: value mismatch for key %x", keySz, key)
			}
		}

		missing := bulkTestKey(rnd, keySz, nil)
		if _, err := byBytes.LoadValueByBytesKey(missing); err != ErrNoSuchKeyInDict {
			// the random missing key may collide with a stored one; regenerate is
			// overkill for 32+ bit spaces, so only assert the not-found contract
			// when the reference agrees the key is absent
			if _, refErr := reference.LoadValue(bulkKeyCell(t, missing, keySz)); refErr == ErrNoSuchKeyInDict {
				t.Fatalf("keySz=%d: expected ErrNoSuchKeyInDict, got %v", keySz, err)
			}
		}

		// delete a missing key (no-op), then delete half of the stored keys
		if err := byBytes.DeleteByBytesKey(missing); err != nil {
			t.Fatal(err)
		}
		if err := reference.Delete(bulkKeyCell(t, missing, keySz)); err != nil {
			t.Fatal(err)
		}
		compareDictRoots(t, reference, byBytes, "after missing delete")

		for i, key := range keys {
			if i%2 != 0 {
				continue
			}
			if err := reference.Delete(bulkKeyCell(t, key, keySz)); err != nil {
				t.Fatal(err)
			}
			if err := byBytes.DeleteByBytesKey(key); err != nil {
				t.Fatal(err)
			}
			compareDictRoots(t, reference, byBytes, "after delete")
		}
	}

	t.Run("short keys rejected", func(t *testing.T) {
		d := NewDict(256)
		if err := d.SetBuilderByBytesKey(make([]byte, 31), BeginCell()); err == nil {
			t.Fatal("expected set error")
		}
		if _, err := d.LoadValueByBytesKey(make([]byte, 31)); err == nil {
			t.Fatal("expected load error")
		}
		if err := d.DeleteByBytesKey(make([]byte, 31)); err == nil {
			t.Fatal("expected delete error")
		}
	})
}
