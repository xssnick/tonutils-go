package cell

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

func drainIteratorKeys(t *testing.T, it *DictIterator, bits uint) []uint64 {
	t.Helper()

	var keys []uint64
	for it.Next() {
		keys = append(keys, it.Key().MustBeginParse().MustLoadUInt(bits))
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator failed: %v", err)
	}
	return keys
}

// expectedTailByScan returns the ordered-scan suffix an iterator positioned at
// query must yield: all keys at-or-after query in iteration order.
func expectedTailByScan(ordered []*Cell, query *Cell, rev bool, allowEq bool, invertFirst bool, bits uint) []uint64 {
	scan := make([]*Cell, len(ordered))
	copy(scan, ordered)
	sort.Slice(scan, func(i, j int) bool {
		less := compareKeyCells(scan[i], scan[j], invertFirst) < 0
		if rev {
			return !less
		}
		return less
	})

	var tail []uint64
	for _, key := range scan {
		cmp := compareKeyCells(key, query, invertFirst)
		if rev {
			cmp = -cmp
		}
		if cmp < 0 || (cmp == 0 && !allowEq) {
			continue
		}
		tail = append(tail, key.MustBeginParse().MustLoadUInt(bits))
	}
	return tail
}

func TestDictionary_IteratorAtMatchesOrderedScanExhaustive(t *testing.T) {
	keys := []uint64{0x00, 0x03, 0x10, 0x3f, 0x40, 0x7f, 0x80, 0xb1, 0xf0, 0xff}
	dict := NewDict(8)
	orderedKeys := make([]*Cell, 0, len(keys))
	for _, key := range keys {
		if err := dict.Set(mustDictKey(t, key, 8), mustDictKey(t, key^0xff, 8)); err != nil {
			t.Fatal(err)
		}
		orderedKeys = append(orderedKeys, mustDictKey(t, key, 8))
	}

	for _, invertFirst := range []bool{false, true} {
		for query := uint64(0); query <= 0xff; query++ {
			queryKey := mustDictKey(t, query, 8)
			for _, rev := range []bool{false, true} {
				for _, allowEq := range []bool{false, true} {
					expected := expectedTailByScan(orderedKeys, queryKey, rev, allowEq, invertFirst, 8)
					it, err := dict.IteratorAt(queryKey, rev, invertFirst, allowEq)
					if err != nil {
						t.Fatalf("query=%02x rev=%v allowEq=%v signed=%v: %v", query, rev, allowEq, invertFirst, err)
					}
					got := drainIteratorKeys(t, it, 8)
					if !equalUint64Slices(got, expected) {
						t.Fatalf("query=%02x rev=%v allowEq=%v signed=%v: got %x, want %x", query, rev, allowEq, invertFirst, got, expected)
					}
				}
			}
		}
	}
}

func TestDictionary_IteratorAtMatchesOrderedScanRandom64(t *testing.T) {
	rnd := rand.New(rand.NewSource(64))

	for round := 0; round < 20; round++ {
		count := 1 + rnd.Intn(60)
		dict := NewDict(64)
		seen := map[uint64]bool{}
		orderedKeys := make([]*Cell, 0, count)
		for len(orderedKeys) < count {
			key := rnd.Uint64()
			if round%3 == 0 {
				// cluster keys to force long shared labels and hml_same edges
				key &= 0xff
				key |= 0xabcdef0011220000
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			if err := dict.Set(mustDictKey(t, key, 64), mustDictKey(t, key^0xff, 64)); err != nil {
				t.Fatal(err)
			}
			orderedKeys = append(orderedKeys, mustDictKey(t, key, 64))
		}

		probes := make([]uint64, 0, 24)
		for i := 0; i < 12; i++ {
			probes = append(probes, rnd.Uint64())
		}
		for _, key := range orderedKeys[:min(12, len(orderedKeys))] {
			probes = append(probes, key.MustBeginParse().MustLoadUInt(64))
		}

		for _, probe := range probes {
			probeKey := mustDictKey(t, probe, 64)
			for _, rev := range []bool{false, true} {
				for _, allowEq := range []bool{false, true} {
					expected := expectedTailByScan(orderedKeys, probeKey, rev, allowEq, false, 64)
					it, err := dict.IteratorAt(probeKey, rev, false, allowEq)
					if err != nil {
						t.Fatalf("round=%d probe=%016x rev=%v allowEq=%v: %v", round, probe, rev, allowEq, err)
					}
					got := drainIteratorKeys(t, it, 64)
					if !equalUint64Slices(got, expected) {
						t.Fatalf("round=%d probe=%016x rev=%v allowEq=%v: got %x, want %x", round, probe, rev, allowEq, got, expected)
					}
				}
			}
		}
	}
}

func TestDictionary_IteratorAtEmptyAndMissing(t *testing.T) {
	var nilDict *Dictionary
	it, err := nilDict.IteratorAt(mustDictKey(t, 1, 8), false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if it.Next() {
		t.Fatal("nil dict iterator must be empty")
	}

	// an empty dict short-circuits before key validation, like LookupNearestKey
	dict := NewDict(8)
	it, err = dict.IteratorAt(mustDictKey(t, 1, 16), false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if it.Next() {
		t.Fatal("empty dict iterator must be empty")
	}

	if err = dict.Set(mustDictKey(t, 1, 8), mustDictKey(t, 1, 8)); err != nil {
		t.Fatal(err)
	}
	if _, err = dict.IteratorAt(mustDictKey(t, 1, 16), false, false, true); err == nil {
		t.Fatal("expected key size mismatch error")
	}
}

// testRefValueAug is a minimal HashmapAug augmentation: extra is an 8-bit
// counter, values are stored in a single ref.
type testRefValueAug struct{}

func (testRefValueAug) SkipExtra(loader *Slice) error {
	_, err := loader.LoadUInt(8)
	return err
}

func (testRefValueAug) EmptyExtra() (*Cell, error) {
	return BeginCell().MustStoreUInt(0, 8).EndCell(), nil
}

func (testRefValueAug) LeafExtra(*Slice) (*Cell, error) {
	return BeginCell().MustStoreUInt(1, 8).EndCell(), nil
}

func (testRefValueAug) CombineExtra(left, right *Slice) (*Cell, error) {
	l, err := left.LoadUInt(8)
	if err != nil {
		return nil, err
	}
	r, err := right.LoadUInt(8)
	if err != nil {
		return nil, err
	}
	return BeginCell().MustStoreUInt((l+r)&0xff, 8).EndCell(), nil
}

func skipOneRefValue(loader *Slice) error {
	return loader.SkipBitsAndRefs(0, 1)
}

// buildInlineAugDictCell serializes the aug dict root node inline surrounded
// by unrelated payload, mirroring an AccountBlock: prefix bits, inline dict,
// trailing bits and a trailing ref.
func buildInlineAugDictCell(t *testing.T, dict *AugmentedDictionary) *Cell {
	t.Helper()

	wrapped, err := dict.ToCell()
	if err != nil {
		t.Fatalf("serialize augmented dict: %v", err)
	}
	loader := wrapped.MustBeginParse()
	hasRoot, err := loader.LoadBoolBit()
	if err != nil || !hasRoot {
		t.Fatalf("augmented dict wrapper has no root: %v", err)
	}
	root, err := loader.LoadRefCell()
	if err != nil {
		t.Fatalf("load augmented dict root: %v", err)
	}

	return BeginCell().
		MustStoreUInt(0x5, 4).
		MustStoreUInt(0xdead, 16).
		MustStoreBuilder(root.ToBuilder()).
		MustStoreRef(BeginCell().MustStoreUInt(0x77, 8).EndCell()).
		EndCell()
}

func TestSlice_AugDictInlineIterator(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))

	for round := 0; round < 12; round++ {
		count := 1 + rnd.Intn(24)
		dict, err := NewAugDict(64, testRefValueAug{})
		if err != nil {
			t.Fatal(err)
		}
		seen := map[uint64]bool{}
		orderedKeys := make([]*Cell, 0, count)
		values := map[uint64]uint64{}
		for len(orderedKeys) < count {
			key := rnd.Uint64() & 0xffff
			if seen[key] {
				continue
			}
			seen[key] = true
			value := rnd.Uint64()
			values[key] = value
			err = dict.Set(
				mustDictKey(t, key, 64),
				BeginCell().MustStoreRef(BeginCell().MustStoreUInt(value, 64).EndCell()).EndCell(),
			)
			if err != nil {
				t.Fatal(err)
			}
			orderedKeys = append(orderedKeys, mustDictKey(t, key, 64))
		}

		container := buildInlineAugDictCell(t, dict)

		for _, rev := range []bool{false, true} {
			loader := container.MustBeginParse()
			if _, err = loader.LoadUInt(4 + 16); err != nil {
				t.Fatal(err)
			}
			it, err := loader.AugDictInlineIterator(64, testRefValueAug{}, skipOneRefValue, rev, false)
			if err != nil {
				t.Fatalf("round=%d rev=%v: %v", round, rev, err)
			}

			// the dict must be fully consumed: exactly the trailing payload left
			if loader.BitsLeft() != 0 || loader.RefsNum() != 1 {
				t.Fatalf("round=%d rev=%v: dict not consumed, %d bits %d refs left", round, rev, loader.BitsLeft(), loader.RefsNum())
			}
			tail, err := loader.LoadRefCell()
			if err != nil {
				t.Fatal(err)
			}
			if got := tail.MustBeginParse().MustLoadUInt(8); got != 0x77 {
				t.Fatalf("round=%d rev=%v: trailing ref mismatch: %x", round, rev, got)
			}

			expected := make([]*Cell, len(orderedKeys))
			copy(expected, orderedKeys)
			sort.Slice(expected, func(i, j int) bool {
				less := compareKeyCells(expected[i], expected[j], false) < 0
				if rev {
					return !less
				}
				return less
			})

			idx := 0
			for it.Next() {
				if idx >= len(expected) {
					t.Fatalf("round=%d rev=%v: too many items", round, rev)
				}
				wantKey := expected[idx].MustBeginParse().MustLoadUInt(64)
				gotKey := it.Key().MustBeginParse().MustLoadUInt(64)
				if gotKey != wantKey {
					t.Fatalf("round=%d rev=%v item=%d: key %x, want %x", round, rev, idx, gotKey, wantKey)
				}
				valueRef, err := it.Value().LoadRefCell()
				if err != nil {
					t.Fatalf("round=%d rev=%v item=%d: load value ref: %v", round, rev, idx, err)
				}
				if got := valueRef.MustBeginParse().MustLoadUInt(64); got != values[gotKey] {
					t.Fatalf("round=%d rev=%v item=%d: value %x, want %x", round, rev, idx, got, values[gotKey])
				}
				if got := it.Extra().MustLoadUInt(8); got != 1 {
					t.Fatalf("round=%d rev=%v item=%d: leaf extra %x, want 1", round, rev, idx, got)
				}
				idx++
			}
			if err = it.Err(); err != nil {
				t.Fatal(err)
			}
			if idx != len(expected) {
				t.Fatalf("round=%d rev=%v: %d items, want %d", round, rev, idx, len(expected))
			}
		}

		// positioned variant against ordered-scan tails
		for probeIdx := 0; probeIdx < 8; probeIdx++ {
			probe := rnd.Uint64() & 0xffff
			if probeIdx%2 == 0 && len(orderedKeys) > 0 {
				probe = orderedKeys[rnd.Intn(len(orderedKeys))].MustBeginParse().MustLoadUInt(64)
			}
			probeKey := mustDictKey(t, probe, 64)
			for _, rev := range []bool{false, true} {
				for _, allowEq := range []bool{false, true} {
					loader := container.MustBeginParse()
					if _, err = loader.LoadUInt(4 + 16); err != nil {
						t.Fatal(err)
					}
					it, err := loader.AugDictInlineIteratorAt(64, testRefValueAug{}, skipOneRefValue, probeKey, rev, false, allowEq)
					if err != nil {
						t.Fatalf("round=%d probe=%x rev=%v allowEq=%v: %v", round, probe, rev, allowEq, err)
					}
					expected := expectedTailByScan(orderedKeys, probeKey, rev, allowEq, false, 64)
					var got []uint64
					for it.Next() {
						got = append(got, it.Key().MustBeginParse().MustLoadUInt(64))
					}
					if err = it.Err(); err != nil {
						t.Fatal(err)
					}
					if !equalUint64Slices(got, expected) {
						t.Fatalf("round=%d probe=%x rev=%v allowEq=%v: got %x, want %x", round, probe, rev, allowEq, got, expected)
					}
				}
			}
		}
	}
}

func TestAugmentedDictionary_IteratorExtraAtMatchesLookupWalk(t *testing.T) {
	rnd := rand.New(rand.NewSource(11))
	dict, err := NewAugDict(64, testRefValueAug{})
	if err != nil {
		t.Fatal(err)
	}
	orderedKeys := make([]*Cell, 0, 40)
	for len(orderedKeys) < 40 {
		key := rnd.Uint64()
		if err = dict.Set(
			mustDictKey(t, key, 64),
			BeginCell().MustStoreRef(BeginCell().MustStoreUInt(key, 64).EndCell()).EndCell(),
		); err != nil {
			t.Fatal(err)
		}
		orderedKeys = append(orderedKeys, mustDictKey(t, key, 64))
	}

	for _, rev := range []bool{false, true} {
		for _, allowEq := range []bool{false, true} {
			probe := orderedKeys[17]
			it, err := dict.IteratorExtraAt(probe, rev, false, allowEq)
			if err != nil {
				t.Fatal(err)
			}

			// reference: repeated LookupNearestKey walk, the pattern the
			// iterator replaces
			var expected []uint64
			current, fetchNext, allowSame := probe, !rev, allowEq
			for {
				key, _, err := dict.LookupNearestKey(current, fetchNext, allowSame, false)
				if err != nil {
					break
				}
				expected = append(expected, key.MustBeginParse().MustLoadUInt(64))
				current, allowSame = key, false
			}

			var got []uint64
			for it.Next() {
				got = append(got, it.Key().MustBeginParse().MustLoadUInt(64))
			}
			if err = it.Err(); err != nil {
				t.Fatal(err)
			}
			if !equalUint64Slices(got, expected) {
				t.Fatalf("rev=%v allowEq=%v: got %x, want %x", rev, allowEq, got, expected)
			}
		}
	}
}

func FuzzDictIteratorAt(f *testing.F) {
	f.Add(uint64(1), uint64(0x80), false, false)
	f.Add(uint64(42), uint64(0), true, true)
	f.Fuzz(func(t *testing.T, seed uint64, probe uint64, rev bool, allowEq bool) {
		rnd := rand.New(rand.NewSource(int64(seed)))
		dict := NewDict(64)
		orderedKeys := make([]*Cell, 0, 16)
		for i := 0; i < 16; i++ {
			key := rnd.Uint64() >> uint(rnd.Intn(56))
			if err := dict.Set(mustDictKey(t, key, 64), mustDictKey(t, key, 64)); err != nil {
				t.Fatal(err)
			}
			orderedKeys = append(orderedKeys, mustDictKey(t, key, 64))
		}
		// dedup for the reference scan
		uniq := map[uint64]*Cell{}
		for _, k := range orderedKeys {
			uniq[k.MustBeginParse().MustLoadUInt(64)] = k
		}
		orderedKeys = orderedKeys[:0]
		for _, k := range uniq {
			orderedKeys = append(orderedKeys, k)
		}

		probeKey := mustDictKey(t, probe, 64)
		it, err := dict.IteratorAt(probeKey, rev, false, allowEq)
		if err != nil {
			t.Fatal(err)
		}
		var got []uint64
		for it.Next() {
			got = append(got, it.Key().MustBeginParse().MustLoadUInt(64))
		}
		if err = it.Err(); err != nil {
			t.Fatal(err)
		}
		expected := expectedTailByScan(orderedKeys, probeKey, rev, allowEq, false, 64)
		if !equalUint64Slices(got, expected) {
			t.Fatal(fmt.Sprintf("seed=%d probe=%x rev=%v allowEq=%v: got %x, want %x", seed, probe, rev, allowEq, got, expected))
		}
	})
}
