package cell

import (
	"errors"
	"math/big"
	"testing"
	"unsafe"
)

type borrowedIntoPlainItem struct {
	key   uint64
	value uint64
}

type borrowedIntoAugItem struct {
	key   uint64
	value uint64
	extra uint64
}

func borrowedIntoAssertSliceEqual(t *testing.T, got, want *Slice) {
	t.Helper()

	gotCell, err := got.ToCell()
	if err != nil {
		t.Fatalf("failed to materialize actual slice: %v", err)
	}
	wantCell, err := want.ToCell()
	if err != nil {
		t.Fatalf("failed to materialize expected slice: %v", err)
	}
	if gotCell.HashKey() != wantCell.HashKey() {
		t.Fatalf("slice mismatch: got %s, want %s", gotCell.Dump(), wantCell.Dump())
	}
}

func borrowedIntoSliceUInt(t *testing.T, value Slice, bits uint) uint64 {
	t.Helper()

	got, err := value.LoadUInt(bits)
	if err != nil {
		t.Fatalf("failed to load %d-bit value: %v", bits, err)
	}
	if value.BitsLeft() != 0 || value.RefsNum() != 0 {
		t.Fatalf("unexpected trailing value data: %d bits, %d refs", value.BitsLeft(), value.RefsNum())
	}
	return got
}

func borrowedIntoPlainItemsEqual(left, right []borrowedIntoPlainItem) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func borrowedIntoAugItemsEqual(left, right []borrowedIntoAugItem) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func borrowedIntoBuildPlainDict(t *testing.T) *Dictionary {
	t.Helper()

	dict := NewDict(8)
	for _, key := range []uint64{0x00, 0x10, 0x7f, 0x80, 0xf0, 0xff} {
		if err := dict.Set(mustDictKey(t, key, 8), mustDictKey(t, key^0xa5, 8)); err != nil {
			t.Fatal(err)
		}
	}
	return dict
}

func borrowedIntoBuildAugDict(t *testing.T) *AugmentedDictionary {
	t.Helper()

	dict, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []uint64{0x00, 0x10, 0x7f, 0x80, 0xf0, 0xff} {
		if err = dict.Set(mustTestAugKey(t, key), mustTestAugValue(t, key^0x5a, 8)); err != nil {
			t.Fatal(err)
		}
	}
	return dict
}

func borrowedIntoCollectPlainOwned(t *testing.T, it *DictIterator) []borrowedIntoPlainItem {
	t.Helper()

	var items []borrowedIntoPlainItem
	for it.Next() {
		item := it.Item()
		items = append(items, borrowedIntoPlainItem{
			key:   item.Key.MustBeginParse().MustLoadUInt(8),
			value: borrowedIntoSliceUInt(t, *item.Value, 8),
		})
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	return items
}

func borrowedIntoCollectPlainViews(t *testing.T, it *DictIterator) []borrowedIntoPlainItem {
	t.Helper()

	var items []borrowedIntoPlainItem
	for it.Next() {
		item := it.View()
		items = append(items, borrowedIntoPlainItem{
			key:   borrowedIntoSliceUInt(t, item.Key, 8),
			value: borrowedIntoSliceUInt(t, item.Value, 8),
		})
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	return items
}

func borrowedIntoCollectAugOwned(t *testing.T, it *AugDictIterator) []borrowedIntoAugItem {
	t.Helper()

	var items []borrowedIntoAugItem
	for it.Next() {
		item := it.Item()
		items = append(items, borrowedIntoAugItem{
			key:   item.Key.MustBeginParse().MustLoadUInt(8),
			value: borrowedIntoSliceUInt(t, *item.Value, 8),
			extra: borrowedIntoSliceUInt(t, *item.Extra, 16),
		})
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	return items
}

func borrowedIntoCollectAugViews(t *testing.T, it *AugDictIterator) []borrowedIntoAugItem {
	t.Helper()

	var items []borrowedIntoAugItem
	for it.Next() {
		item := it.View()
		items = append(items, borrowedIntoAugItem{
			key:   borrowedIntoSliceUInt(t, item.Key, 8),
			value: borrowedIntoSliceUInt(t, item.Value, 8),
			extra: borrowedIntoSliceUInt(t, item.Extra, 16),
		})
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	return items
}

func TestDictionaryLoadValueIntoParityAndReuse(t *testing.T) {
	dict := NewDict(8)
	values := map[uint64]*Cell{
		0x10: BeginCell().MustStoreUInt(0xaa, 8).EndCell(),
		0x20: BeginCell().MustStoreUInt(0xabc, 12).MustStoreRef(BeginCell().MustStoreUInt(0x55, 8).EndCell()).EndCell(),
	}
	for key, value := range values {
		if err := dict.Set(mustDictKey(t, key, 8), value); err != nil {
			t.Fatal(err)
		}
	}

	dst := *BeginCell().MustStoreUInt(0xffff, 16).MustStoreRef(BeginCell().EndCell()).EndCell().MustBeginParse()
	for _, key := range []uint64{0x10, 0x20, 0x10} {
		want, err := dict.LoadValue(mustDictKey(t, key, 8))
		if err != nil {
			t.Fatal(err)
		}
		if err = dict.LoadValueInto(mustDictKey(t, key, 8), &dst); err != nil {
			t.Fatal(err)
		}
		borrowedIntoAssertSliceEqual(t, &dst, want)
	}

	var missing Slice
	if err := dict.LoadValueInto(mustDictKey(t, 0x30, 8), &missing); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("missing Into lookup returned %v", err)
	}
	if _, err := dict.LoadValue(mustDictKey(t, 0x30, 8)); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("missing owned lookup returned %v", err)
	}

	if err := dict.LoadValueInto(mustDictKey(t, 0x10, 7), &dst); err == nil {
		t.Fatal("Into lookup accepted an incorrect key size")
	}
}

func TestDictionaryLoadValueIntoMalformedSpecialAndTraceParity(t *testing.T) {
	key := mustDictKey(t, 0x10, 8)

	malformed := BeginCell().EndCell().AsDict(8)
	var dst Slice
	if _, err := malformed.LoadValue(key); err == nil {
		t.Fatal("owned lookup accepted a malformed dictionary")
	}
	if err := malformed.LoadValueInto(key, &dst); err == nil {
		t.Fatal("Into lookup accepted a malformed dictionary")
	}

	special := makeManualCellForTest(true, LevelMask{}, 8, []byte{byte(LibraryCellType)}, nil).AsDict(8)
	if _, err := special.LoadValue(key); !errors.Is(err, ErrDictHasSpecialCells) {
		t.Fatalf("owned special-cell lookup returned %v", err)
	}
	if err := special.LoadValueInto(key, &dst); !errors.Is(err, ErrDictHasSpecialCells) {
		t.Fatalf("Into special-cell lookup returned %v", err)
	}

	base := borrowedIntoBuildPlainDict(t)
	countLoads := func(into bool) int {
		loads := 0
		var trace *Trace
		trace = NewTrace(TraceHooks{
			OnLoad: func(*Cell) { loads++ },
			OnChild: func(int) *Trace {
				return trace
			},
		})
		traced := base.Copy()
		traced.root = base.root.WithTrace(trace)
		traced.trace = nil
		if into {
			var value Slice
			if err := traced.LoadValueInto(key, &value); err != nil {
				t.Fatal(err)
			}
		} else if _, err := traced.LoadValue(key); err != nil {
			t.Fatal(err)
		}
		return loads
	}
	ownedLoads := countLoads(false)
	intoLoads := countLoads(true)
	if ownedLoads == 0 || intoLoads != ownedLoads {
		t.Fatalf("trace mismatch: owned=%d Into=%d", ownedLoads, intoLoads)
	}
}

func TestPrefixDictionaryIntoParityReuseAndMiss(t *testing.T) {
	dict := NewPrefixDict(8)
	for _, item := range []struct {
		key   uint64
		bits  uint
		value uint64
	}{
		{key: 0b10, bits: 2, value: 0xa2},
		{key: 0b011, bits: 3, value: 0xb3},
		{key: 0b1110, bits: 4, value: 0xc4},
	} {
		if err := dict.Set(mustDictKey(t, item.key, item.bits), mustDictKey(t, item.value, 8)); err != nil {
			t.Fatal(err)
		}
	}

	dst := *BeginCell().MustStoreUInt(0xffff, 16).EndCell().MustBeginParse()
	for _, query := range []uint64{0b10110101, 0b01110101, 0b11101111} {
		key := mustDictKey(t, query, 8)
		want, wantMatched, err := dict.LookupPrefix(key)
		if err != nil || want == nil {
			t.Fatalf("owned prefix lookup failed: matched=%d err=%v", wantMatched, err)
		}
		matched, err := dict.LookupPrefixInto(key, &dst)
		if err != nil {
			t.Fatal(err)
		}
		if matched != wantMatched {
			t.Fatalf("matched=%d, want %d", matched, wantMatched)
		}
		borrowedIntoAssertSliceEqual(t, &dst, want)
	}

	fullKey := mustDictKey(t, 0b1110, 4)
	want, err := dict.LoadValue(fullKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = dict.LoadValueInto(fullKey, &dst); err != nil {
		t.Fatal(err)
	}
	borrowedIntoAssertSliceEqual(t, &dst, want)

	partialOnly := mustDictKey(t, 0b1011, 4)
	beforePartialMiss := dst
	if err = dict.LoadValueInto(partialOnly, &dst); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("full-width lookup of a shorter stored prefix returned %v", err)
	}
	if dst != beforePartialMiss {
		t.Fatal("full-width prefix miss modified the destination")
	}
	beforeIntPartialMiss := dst
	if err = dict.LoadValueByIntKeyInto(big.NewInt(0b10110101), &dst); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("integer lookup of a shorter stored prefix returned %v", err)
	}
	if dst != beforeIntPartialMiss {
		t.Fatal("integer full-width prefix miss modified the destination")
	}

	malformed := BeginCell().EndCell().AsPrefixDict(4)
	beforeMalformed := dst
	if err = malformed.LoadValueInto(mustDictKey(t, 0b1011, 4), &dst); err == nil {
		t.Fatal("full-width lookup accepted a malformed prefix dictionary")
	}
	if dst != beforeMalformed {
		t.Fatal("malformed full-width prefix lookup modified the destination")
	}

	missingKey := mustDictKey(t, 0b00110101, 8)
	owned, ownedMatched, err := dict.LookupPrefix(missingKey)
	if err != nil || owned != nil {
		t.Fatalf("legacy miss result: value=%v matched=%d err=%v", owned, ownedMatched, err)
	}
	matched, err := dict.LookupPrefixInto(missingKey, &dst)
	if !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("Into miss returned matched=%d err=%v", matched, err)
	}
	if matched != ownedMatched {
		t.Fatalf("miss matched=%d, legacy matched=%d", matched, ownedMatched)
	}
}

func TestAugmentedDictionaryIntoParityReuseAndAliases(t *testing.T) {
	dict, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		key   uint64
		value *Cell
	}{
		{key: 0x10, value: BeginCell().MustStoreUInt(0xaa, 8).EndCell()},
		{key: 0x20, value: BeginCell().MustStoreUInt(0xabc, 12).MustStoreRef(BeginCell().MustStoreUInt(0x55, 8).EndCell()).EndCell()},
	} {
		if err = dict.Set(mustTestAugKey(t, item.key), item.value); err != nil {
			t.Fatal(err)
		}
	}

	key := mustTestAugKey(t, 0x20)
	rawWant, err := dict.LoadValueWithExtra(key)
	if err != nil {
		t.Fatal(err)
	}
	valueWant, extraWant, err := dict.LoadValueExtra(key)
	if err != nil {
		t.Fatal(err)
	}

	rawDst := *BeginCell().MustStoreUInt(0xffff, 16).MustStoreRef(BeginCell().EndCell()).EndCell().MustBeginParse()
	valueDst := rawDst
	extraDst := rawDst
	if err = dict.LoadValueWithExtraInto(key, &rawDst); err != nil {
		t.Fatal(err)
	}
	borrowedIntoAssertSliceEqual(t, &rawDst, rawWant)
	if err = dict.LoadValueInto(key, &valueDst); err != nil {
		t.Fatal(err)
	}
	borrowedIntoAssertSliceEqual(t, &valueDst, valueWant)
	if err = dict.LoadValueExtraInto(key, &valueDst, &extraDst); err != nil {
		t.Fatal(err)
	}
	borrowedIntoAssertSliceEqual(t, &valueDst, valueWant)
	borrowedIntoAssertSliceEqual(t, &extraDst, extraWant)

	intKey := big.NewInt(0x20)
	if err = dict.LoadValueWithExtraByIntKeyInto(intKey, &rawDst); err != nil {
		t.Fatal(err)
	}
	borrowedIntoAssertSliceEqual(t, &rawDst, rawWant)
	if err = dict.LoadValueByIntKeyInto(intKey, &valueDst); err != nil {
		t.Fatal(err)
	}
	borrowedIntoAssertSliceEqual(t, &valueDst, valueWant)
	if err = dict.LoadValueExtraByIntKeyInto(intKey, &valueDst, &extraDst); err != nil {
		t.Fatal(err)
	}
	borrowedIntoAssertSliceEqual(t, &valueDst, valueWant)
	borrowedIntoAssertSliceEqual(t, &extraDst, extraWant)

	aliased := *rawWant
	var aliasExtra Slice
	if err = dict.decomposeValueExtraInto(&aliased, &aliased, &aliasExtra); err != nil {
		t.Fatal(err)
	}
	borrowedIntoAssertSliceEqual(t, &aliased, valueWant)
	borrowedIntoAssertSliceEqual(t, &aliasExtra, extraWant)

	same := rawDst
	if err = dict.LoadValueExtraInto(key, &same, &same); err == nil {
		t.Fatal("LoadValueExtraInto accepted identical value and extra destinations")
	}

	missing := mustTestAugKey(t, 0x30)
	if err = dict.LoadValueWithExtraInto(missing, &rawDst); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("raw missing lookup returned %v", err)
	}
	if err = dict.LoadValueInto(missing, &valueDst); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("value missing lookup returned %v", err)
	}
	if err = dict.LoadValueExtraInto(missing, &valueDst, &extraDst); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("decomposed missing lookup returned %v", err)
	}
}

func TestAugmentedDictionaryDecomposeIntoErrorLeavesDestinationsUnchanged(t *testing.T) {
	t.Run("malformed extra", func(t *testing.T) {
		dict := &AugmentedDictionary{aug: testMetricAugmentation{}}
		valueExtra := BeginCell().MustStoreUInt(0xab, 8).EndCell().MustBeginParse()
		value := *BeginCell().MustStoreUInt(0x1234, 16).EndCell().MustBeginParse()
		extra := *BeginCell().MustStoreUInt(0x5678, 16).EndCell().MustBeginParse()
		valueBefore, extraBefore := value, extra

		if err := dict.decomposeValueExtraInto(valueExtra, &value, &extra); err == nil {
			t.Fatal("decomposition accepted a truncated extra")
		}
		if value != valueBefore || extra != extraBefore {
			t.Fatal("truncated extra modified decomposition destinations")
		}
	})

	t.Run("skipper error with aliased value", func(t *testing.T) {
		wantErr := errors.New("skip extra failed")
		dict := &AugmentedDictionary{aug: ReadOnlyAugmentation{SkipExtraFn: func(loader *Slice) error {
			if _, err := loader.LoadUInt(4); err != nil {
				return err
			}
			return wantErr
		}}}
		valueExtra := *BeginCell().MustStoreUInt(0xabc, 12).EndCell().MustBeginParse()
		extra := *BeginCell().MustStoreUInt(0x5678, 16).EndCell().MustBeginParse()
		valueBefore, extraBefore := valueExtra, extra

		err := dict.decomposeValueExtraInto(&valueExtra, &valueExtra, &extra)
		if !errors.Is(err, wantErr) {
			t.Fatalf("decomposition returned %v, want %v", err, wantErr)
		}
		if valueExtra != valueBefore || extra != extraBefore {
			t.Fatal("skipper error modified aliased decomposition destinations")
		}
	})
}

func TestDictIteratorViewKeyToCellReturnsOwnedFinalizedCell(t *testing.T) {
	wantSliceSize := uintptr(16)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		wantSliceSize = 24
	}
	if got := unsafe.Sizeof(Slice{}); got != wantSliceSize {
		t.Fatalf("Slice size = %d, want %d", got, wantSliceSize)
	}

	dict := borrowedIntoBuildPlainDict(t)
	it, err := dict.Iterator(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !it.Next() {
		t.Fatalf("iterator has no first item: %v", it.Err())
	}

	view := it.View()
	keyViewCopy := view.Key.Copy()
	preloaded, err := view.Key.PreloadSubslice(view.Key.BitsLeft(), view.Key.RefsNum())
	if err != nil {
		t.Fatal(err)
	}
	fetchSource := view.Key
	fetched, err := fetchSource.FetchSubslice(fetchSource.BitsLeft(), fetchSource.RefsNum())
	if err != nil {
		t.Fatal(err)
	}
	advanced := view.Key
	if _, err = advanced.LoadUInt(3); err != nil {
		t.Fatal(err)
	}

	want := mustDictKey(t, 0x00, 8)
	materialize := func(name string, keyView *Slice) *Cell {
		t.Helper()

		key, err := keyView.ToCell()
		if err != nil {
			t.Fatal(err)
		}
		if key == keyView.RawCell() {
			t.Fatalf("%s ToCell returned the iterator scratch cell", name)
		}
		if key.HashKey() != want.HashKey() {
			t.Fatalf("%s hash = %x, want %x", name, key.Hash(), want.Hash())
		}
		return key
	}

	retained := map[string]*Cell{
		"direct key":    materialize("borrowed key", &view.Key),
		"copied key":    materialize("copied borrowed key", keyViewCopy),
		"preloaded key": materialize("preloaded borrowed key", preloaded),
		"fetched key":   materialize("fetched borrowed key", fetched),
		"advanced base": advanced.BaseCell(),
	}
	if retained["advanced base"] == advanced.RawCell() {
		t.Fatal("BaseCell returned the iterator scratch cell")
	}
	if retained["advanced base"].BitsSize() != 8 {
		t.Fatalf("advanced BaseCell has %d bits, want the full 8-bit base", retained["advanced base"].BitsSize())
	}

	if !it.Next() {
		t.Fatalf("iterator has no second item: %v", it.Err())
	}
	for name, key := range retained {
		if key.HashKey() != want.HashKey() || key.MustBeginParse().MustLoadUInt(8) != 0x00 {
			t.Fatalf("materialized %s changed after iterator advance", name)
		}
	}
}

func TestDictionaryBorrowedTraversalOrderAndIteratorAtParity(t *testing.T) {
	dict := borrowedIntoBuildPlainDict(t)
	for _, rev := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			name := "forward_unsigned"
			if rev {
				name = "reverse_unsigned"
			}
			if signed {
				name += "_signed"
			}
			t.Run(name, func(t *testing.T) {
				ownedIt, err := dict.Iterator(rev, signed)
				if err != nil {
					t.Fatal(err)
				}
				owned := borrowedIntoCollectPlainOwned(t, ownedIt)

				viewIt, err := dict.Iterator(rev, signed)
				if err != nil {
					t.Fatal(err)
				}
				views := borrowedIntoCollectPlainViews(t, viewIt)
				if !borrowedIntoPlainItemsEqual(views, owned) {
					t.Fatalf("view order/content = %v, want %v", views, owned)
				}

				var foreach []borrowedIntoPlainItem
				if err = dict.ForEachBorrowed(rev, signed, func(item DictItemView) error {
					foreach = append(foreach, borrowedIntoPlainItem{
						key:   borrowedIntoSliceUInt(t, item.Key, 8),
						value: borrowedIntoSliceUInt(t, item.Value, 8),
					})
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if !borrowedIntoPlainItemsEqual(foreach, owned) {
					t.Fatalf("borrowed callback order/content = %v, want %v", foreach, owned)
				}
			})
		}
	}

	query := mustDictKey(t, 0x7f, 8)
	for _, rev := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			for _, allowEq := range []bool{false, true} {
				ownedIt, err := dict.IteratorAt(query, rev, signed, allowEq)
				if err != nil {
					t.Fatal(err)
				}
				owned := borrowedIntoCollectPlainOwned(t, ownedIt)
				viewIt, err := dict.IteratorAt(query, rev, signed, allowEq)
				if err != nil {
					t.Fatal(err)
				}
				views := borrowedIntoCollectPlainViews(t, viewIt)
				if !borrowedIntoPlainItemsEqual(views, owned) {
					t.Fatalf("IteratorAt rev=%v signed=%v allowEq=%v: got %v, want %v", rev, signed, allowEq, views, owned)
				}
			}
		}
	}
}

func TestAugmentedDictionaryBorrowedTraversalOrderAndIteratorAtParity(t *testing.T) {
	dict := borrowedIntoBuildAugDict(t)
	for _, rev := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			ownedIt, err := dict.IteratorExtra(rev, signed)
			if err != nil {
				t.Fatal(err)
			}
			owned := borrowedIntoCollectAugOwned(t, ownedIt)

			viewIt, err := dict.IteratorExtra(rev, signed)
			if err != nil {
				t.Fatal(err)
			}
			views := borrowedIntoCollectAugViews(t, viewIt)
			if !borrowedIntoAugItemsEqual(views, owned) {
				t.Fatalf("rev=%v signed=%v views=%v, want %v", rev, signed, views, owned)
			}

			var foreach []borrowedIntoAugItem
			if err = dict.ForEachBorrowed(rev, signed, func(item AugDictItemView) error {
				foreach = append(foreach, borrowedIntoAugItem{
					key:   borrowedIntoSliceUInt(t, item.Key, 8),
					value: borrowedIntoSliceUInt(t, item.Value, 8),
					extra: borrowedIntoSliceUInt(t, item.Extra, 16),
				})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !borrowedIntoAugItemsEqual(foreach, owned) {
				t.Fatalf("rev=%v signed=%v callback=%v, want %v", rev, signed, foreach, owned)
			}
		}
	}

	query := mustTestAugKey(t, 0x7f)
	for _, rev := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			for _, allowEq := range []bool{false, true} {
				ownedIt, err := dict.IteratorExtraAt(query, rev, signed, allowEq)
				if err != nil {
					t.Fatal(err)
				}
				owned := borrowedIntoCollectAugOwned(t, ownedIt)
				viewIt, err := dict.IteratorExtraAt(query, rev, signed, allowEq)
				if err != nil {
					t.Fatal(err)
				}
				views := borrowedIntoCollectAugViews(t, viewIt)
				if !borrowedIntoAugItemsEqual(views, owned) {
					t.Fatalf("IteratorExtraAt rev=%v signed=%v allowEq=%v: got %v, want %v", rev, signed, allowEq, views, owned)
				}
			}
		}
	}
}

func TestBorrowedIteratorsPreserveLastItemAndCallbackErrors(t *testing.T) {
	dict := borrowedIntoBuildPlainDict(t)
	it, err := dict.Iterator(false, false)
	if err != nil {
		t.Fatal(err)
	}
	var last borrowedIntoPlainItem
	for it.Next() {
		view := it.View()
		last = borrowedIntoPlainItem{
			key:   borrowedIntoSliceUInt(t, view.Key, 8),
			value: borrowedIntoSliceUInt(t, view.Value, 8),
		}
	}
	item := it.Item()
	gotLast := borrowedIntoPlainItem{
		key:   item.Key.MustBeginParse().MustLoadUInt(8),
		value: borrowedIntoSliceUInt(t, *item.Value, 8),
	}
	if gotLast != last {
		t.Fatalf("Item after final Next(false) = %v, want %v", gotLast, last)
	}

	wantErr := errors.New("stop borrowed walk")
	calls := 0
	if err = dict.ForEachBorrowed(false, false, func(DictItemView) error {
		calls++
		if calls == 2 {
			return wantErr
		}
		return nil
	}); !errors.Is(err, wantErr) || calls != 2 {
		t.Fatalf("plain callback stop: calls=%d err=%v", calls, err)
	}

	aug := borrowedIntoBuildAugDict(t)
	augIt, err := aug.IteratorExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	var lastAug borrowedIntoAugItem
	for augIt.Next() {
		view := augIt.View()
		lastAug = borrowedIntoAugItem{
			key:   borrowedIntoSliceUInt(t, view.Key, 8),
			value: borrowedIntoSliceUInt(t, view.Value, 8),
			extra: borrowedIntoSliceUInt(t, view.Extra, 16),
		}
	}
	augItem := augIt.Item()
	gotLastAug := borrowedIntoAugItem{
		key:   augItem.Key.MustBeginParse().MustLoadUInt(8),
		value: borrowedIntoSliceUInt(t, *augItem.Value, 8),
		extra: borrowedIntoSliceUInt(t, *augItem.Extra, 16),
	}
	if gotLastAug != lastAug {
		t.Fatalf("aug Item after final Next(false) = %v, want %v", gotLastAug, lastAug)
	}

	calls = 0
	if err = aug.ForEachBorrowed(false, false, func(AugDictItemView) error {
		calls++
		if calls == 3 {
			return wantErr
		}
		return nil
	}); !errors.Is(err, wantErr) || calls != 3 {
		t.Fatalf("aug callback stop: calls=%d err=%v", calls, err)
	}
}

func TestBorrowedTraversalTraceLazyAndSpecialParity(t *testing.T) {
	plain := borrowedIntoBuildPlainDict(t)
	countPlainLoads := func(borrowed bool) int {
		loads := 0
		var trace *Trace
		trace = NewTrace(TraceHooks{
			OnLoad: func(*Cell) { loads++ },
			OnChild: func(int) *Trace {
				return trace
			},
		})
		traced := plain.Copy()
		traced.root = plain.root.WithTrace(trace)
		traced.trace = nil
		it, err := traced.Iterator(false, false)
		if err != nil {
			t.Fatal(err)
		}
		if borrowed {
			_ = borrowedIntoCollectPlainViews(t, it)
		} else {
			_ = borrowedIntoCollectPlainOwned(t, it)
		}
		return loads
	}
	plainOwnedLoads := countPlainLoads(false)
	plainBorrowedLoads := countPlainLoads(true)
	if plainOwnedLoads == 0 || plainBorrowedLoads != plainOwnedLoads {
		t.Fatalf("plain traversal trace mismatch: owned=%d borrowed=%d", plainOwnedLoads, plainBorrowedLoads)
	}

	plainLoader := testLazyLoaderForCellTree(plain.root)
	lazyPlain := plain.Copy()
	lazyPlain.root = cellWithLazyRefsFromCell(plain.root, plainLoader.LoadCell)
	var lazyPlainItems []borrowedIntoPlainItem
	if err := lazyPlain.ForEachBorrowed(false, false, func(item DictItemView) error {
		lazyPlainItems = append(lazyPlainItems, borrowedIntoPlainItem{
			key:   borrowedIntoSliceUInt(t, item.Key, 8),
			value: borrowedIntoSliceUInt(t, item.Value, 8),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	eagerPlainIt, err := plain.Iterator(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if eager := borrowedIntoCollectPlainViews(t, eagerPlainIt); !borrowedIntoPlainItemsEqual(lazyPlainItems, eager) {
		t.Fatalf("lazy plain traversal=%v, eager=%v", lazyPlainItems, eager)
	}
	if plainLoader.calls == 0 {
		t.Fatal("borrowed plain traversal did not load lazy branches")
	}

	aug := borrowedIntoBuildAugDict(t)
	countAugLoads := func(borrowed bool) int {
		loads := 0
		var trace *Trace
		trace = NewTrace(TraceHooks{
			OnLoad: func(*Cell) { loads++ },
			OnChild: func(int) *Trace {
				return trace
			},
		})
		traced := aug.Copy()
		traced.root = aug.root.WithTrace(trace)
		traced.trace = nil
		it, err := traced.IteratorExtra(false, false)
		if err != nil {
			t.Fatal(err)
		}
		if borrowed {
			_ = borrowedIntoCollectAugViews(t, it)
		} else {
			_ = borrowedIntoCollectAugOwned(t, it)
		}
		return loads
	}
	augOwnedLoads := countAugLoads(false)
	augBorrowedLoads := countAugLoads(true)
	if augOwnedLoads == 0 || augBorrowedLoads != augOwnedLoads {
		t.Fatalf("aug traversal trace mismatch: owned=%d borrowed=%d", augOwnedLoads, augBorrowedLoads)
	}

	augLoader := testLazyLoaderForCellTree(aug.root)
	lazyAug := aug.Copy()
	lazyAug.root = cellWithLazyRefsFromCell(aug.root, augLoader.LoadCell)
	var lazyAugItems []borrowedIntoAugItem
	if err = lazyAug.ForEachBorrowed(false, false, func(item AugDictItemView) error {
		lazyAugItems = append(lazyAugItems, borrowedIntoAugItem{
			key:   borrowedIntoSliceUInt(t, item.Key, 8),
			value: borrowedIntoSliceUInt(t, item.Value, 8),
			extra: borrowedIntoSliceUInt(t, item.Extra, 16),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	eagerAugIt, err := aug.IteratorExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if eager := borrowedIntoCollectAugViews(t, eagerAugIt); !borrowedIntoAugItemsEqual(lazyAugItems, eager) {
		t.Fatalf("lazy aug traversal=%v, eager=%v", lazyAugItems, eager)
	}
	if augLoader.calls == 0 {
		t.Fatal("borrowed augmented traversal did not load lazy branches")
	}

	specialRoot := makeManualCellForTest(true, LevelMask{}, 8, []byte{byte(LibraryCellType)}, nil)
	if err = specialRoot.AsDict(8).ForEachBorrowed(false, false, func(DictItemView) error { return nil }); !errors.Is(err, ErrDictHasSpecialCells) {
		t.Fatalf("plain special-cell traversal returned %v", err)
	}
	if err = specialRoot.AsAugDict(8, testMetricAugmentation{}).ForEachBorrowed(false, false, func(AugDictItemView) error { return nil }); !errors.Is(err, ErrDictHasSpecialCells) {
		t.Fatalf("aug special-cell traversal returned %v", err)
	}
}
