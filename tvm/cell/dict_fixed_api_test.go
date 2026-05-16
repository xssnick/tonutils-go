package cell

import (
	"errors"
	"sort"
	"testing"
)

func mustCollectDictKeys(t *testing.T, items []DictItem, bits uint) []uint64 {
	t.Helper()

	keys := make([]uint64, len(items))
	for i, item := range items {
		keys[i] = item.Key.MustBeginParse().MustLoadUInt(bits)
	}
	return keys
}

func mustDictKey(t *testing.T, value uint64, bits uint) *Cell {
	t.Helper()
	return BeginCell().MustStoreUInt(value, bits).EndCell()
}

func TestDictionary_RangeIteratorNearestAndCommonPrefix(t *testing.T) {
	dict := NewDict(8)
	for _, pair := range []struct {
		key uint64
		val uint64
	}{
		{0x10, 0xa1},
		{0x7f, 0xb2},
		{0x80, 0xc3},
		{0xf0, 0xd4},
	} {
		if err := dict.Set(mustDictKey(t, pair.key, 8), mustDictKey(t, pair.val, 8)); err != nil {
			t.Fatal(err)
		}
	}

	items, err := dict.Range(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCollectDictKeys(t, items, 8); !equalUint64Slices(got, []uint64{0x10, 0x7f, 0x80, 0xf0}) {
		t.Fatalf("unexpected unsigned range order: %v", got)
	}

	items, err = dict.Range(false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCollectDictKeys(t, items, 8); !equalUint64Slices(got, []uint64{0x80, 0xf0, 0x10, 0x7f}) {
		t.Fatalf("unexpected signed range order: %v", got)
	}

	it, err := dict.Iterator(true, false)
	if err != nil {
		t.Fatal(err)
	}
	var reverse []uint64
	for it.Next() {
		reverse = append(reverse, it.Key().MustBeginParse().MustLoadUInt(8))
	}
	if !equalUint64Slices(reverse, []uint64{0xf0, 0x80, 0x7f, 0x10}) {
		t.Fatalf("unexpected reverse iterator order: %v", reverse)
	}

	key, value, err := dict.LookupNearestKey(mustDictKey(t, 0x81, 8), false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := key.MustBeginParse().MustLoadUInt(8); got != 0x80 {
		t.Fatalf("unexpected previous nearest key: %x", got)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0xc3 {
		t.Fatalf("unexpected previous nearest value: %x", got)
	}

	key, _, err = dict.LookupNearestKey(mustDictKey(t, 0x81, 8), true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := key.MustBeginParse().MustLoadUInt(8); got != 0xf0 {
		t.Fatalf("unexpected next nearest key: %x", got)
	}

	key, _, err = dict.LookupNearestKey(mustDictKey(t, 0x10, 8), false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := key.MustBeginParse().MustLoadUInt(8); got != 0xf0 {
		t.Fatalf("unexpected signed previous nearest key: %x", got)
	}

	key, _, err = dict.LookupNearestKey(mustDictKey(t, 0x10, 8), true, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := key.MustBeginParse().MustLoadUInt(8); got != 0x7f {
		t.Fatalf("unexpected signed next nearest key: %x", got)
	}

	pfxDict := NewDict(8)
	for _, key := range []uint64{0x80, 0x81, 0x8f} {
		if err := pfxDict.Set(mustDictKey(t, key, 8), mustDictKey(t, key, 8)); err != nil {
			t.Fatal(err)
		}
	}

	ok, err := pfxDict.HasCommonPrefix(BeginCell().MustStoreUInt(0b1000, 4).EndCell())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected common prefix to match")
	}

	ok, err = pfxDict.HasCommonPrefix(BeginCell().MustStoreUInt(0b1001, 4).EndCell())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected common prefix mismatch")
	}

	common, err := pfxDict.GetCommonPrefix()
	if err != nil {
		t.Fatal(err)
	}
	if got := common.MustBeginParse().MustLoadUInt(4); got != 0b1000 {
		t.Fatalf("unexpected common prefix bits: %b", got)
	}
}

func TestDictionary_LookupNearestKeyMatchesOrderedScan(t *testing.T) {
	keys := []uint64{0x00, 0x03, 0x10, 0x3f, 0x40, 0x7f, 0x80, 0xb1, 0xf0, 0xff}
	dict := NewDict(8)
	for _, key := range keys {
		if err := dict.Set(mustDictKey(t, key, 8), mustDictKey(t, key^0xff, 8)); err != nil {
			t.Fatal(err)
		}
	}

	for _, invertFirst := range []bool{false, true} {
		ordered := make([]*Cell, len(keys))
		for i, key := range keys {
			ordered[i] = mustDictKey(t, key, 8)
		}
		sort.Slice(ordered, func(i, j int) bool {
			return compareKeyCells(ordered[i], ordered[j], invertFirst) < 0
		})

		for query := uint64(0); query <= 0xff; query++ {
			queryKey := mustDictKey(t, query, 8)
			for _, fetchNext := range []bool{false, true} {
				for _, allowEq := range []bool{false, true} {
					expected := nearestByScan(ordered, queryKey, fetchNext, allowEq, invertFirst)
					gotKey, gotValue, err := dict.LookupNearestKey(queryKey, fetchNext, allowEq, invertFirst)
					if expected == nil {
						if !errors.Is(err, ErrNoSuchKeyInDict) {
							t.Fatalf("query=%02x fetchNext=%v allowEq=%v signed=%v: expected miss, got key=%v value=%v err=%v", query, fetchNext, allowEq, invertFirst, gotKey, gotValue, err)
						}
						continue
					}
					if err != nil {
						t.Fatalf("query=%02x fetchNext=%v allowEq=%v signed=%v: %v", query, fetchNext, allowEq, invertFirst, err)
					}
					if compareKeyCells(gotKey, expected, false) != 0 {
						t.Fatalf("query=%02x fetchNext=%v allowEq=%v signed=%v: key=%02x, want %02x", query, fetchNext, allowEq, invertFirst, gotKey.MustBeginParse().MustLoadUInt(8), expected.MustBeginParse().MustLoadUInt(8))
					}
					if got := mustLoadTestValue(t, gotValue, 8); got != gotKey.MustBeginParse().MustLoadUInt(8)^0xff {
						t.Fatalf("query=%02x fetchNext=%v allowEq=%v signed=%v: value=%02x does not match key", query, fetchNext, allowEq, invertFirst, got)
					}
				}
			}
		}
	}
}

func nearestByScan(ordered []*Cell, query *Cell, fetchNext bool, allowEq bool, invertFirst bool) *Cell {
	pos := sort.Search(len(ordered), func(i int) bool {
		return compareKeyCells(ordered[i], query, invertFirst) >= 0
	})
	if pos < len(ordered) && compareKeyCells(ordered[pos], query, invertFirst) == 0 {
		if allowEq {
			return ordered[pos]
		}
		if fetchNext {
			pos++
		} else {
			pos--
		}
	} else if !fetchNext {
		pos--
	}
	if pos < 0 || pos >= len(ordered) {
		return nil
	}
	return ordered[pos]
}

func TestDictionary_PrefixSubdictAndFilter(t *testing.T) {
	dict := NewDict(4)
	for _, pair := range []struct {
		key uint64
		val uint64
	}{
		{0b1000, 0xa0},
		{0b1001, 0xa1},
		{0b0011, 0xb3},
	} {
		if err := dict.Set(mustDictKey(t, pair.key, 4), mustDictKey(t, pair.val, 8)); err != nil {
			t.Fatal(err)
		}
	}

	subRoot, err := dict.ExtractPrefixSubdictRoot(BeginCell().MustStoreUInt(0b10, 2).EndCell(), false)
	if err != nil {
		t.Fatal(err)
	}
	subDict := subRoot.AsDict(4)
	subItems, err := subDict.Range(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCollectDictKeys(t, subItems, 4); !equalUint64Slices(got, []uint64{0b1000, 0b1001}) {
		t.Fatalf("unexpected extracted subdict keys: %v", got)
	}

	cutDict := dict.Copy()
	ok, err := cutDict.CutPrefixSubdict(BeginCell().MustStoreUInt(0b10, 2).EndCell(), true)
	if err != nil || !ok {
		t.Fatalf("failed to cut prefix subdict: ok=%v err=%v", ok, err)
	}
	if cutDict.GetKeySize() != 2 {
		t.Fatalf("unexpected key size after prefix cut: %d", cutDict.GetKeySize())
	}
	cutItems, err := cutDict.Range(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCollectDictKeys(t, cutItems, 2); !equalUint64Slices(got, []uint64{0b00, 0b01}) {
		t.Fatalf("unexpected cut subdict keys: %v", got)
	}

	changes, err := dict.Filter(func(value *Slice, key *Cell) (DictFilterAction, error) {
		if key.MustBeginParse().MustLoadUInt(4) == 0b0011 {
			return DictFilterRemove, nil
		}
		return DictFilterKeep, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Fatalf("unexpected filter changes: %d", changes)
	}

	ok, err = dict.CheckForEach(func(_ *Slice, _ *Cell) (bool, error) {
		return true, nil
	}, false, false)
	if err != nil || !ok {
		t.Fatalf("check_for_each failed: ok=%v err=%v", ok, err)
	}

	ok, err = dict.ValidateCheck(func(_ *Slice, _ *Cell) (bool, error) {
		return true, nil
	}, false)
	if err != nil || !ok {
		t.Fatalf("validate_check failed: ok=%v err=%v", ok, err)
	}
	if !dict.ValidateAll() {
		t.Fatal("validate_all failed for a valid dictionary")
	}
}

func equalUint64Slices(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
