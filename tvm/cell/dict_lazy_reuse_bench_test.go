package cell

import "testing"

// These fixtures model a loader backed by a decoded-cell cache: repeated
// resolutions still call the loader and validate the boundary, but do not
// allocate or decode another cell. loads/op exposes that work separately.
func benchmarkDictCachedLazyRoot(tb testing.TB, root *Cell, calls *uint64) *Cell {
	tb.Helper()
	store := make(map[Hash]*Cell)
	load := func(hash Hash) (*Cell, error) {
		*calls++
		return store[hash], nil
	}
	var visit func(*Cell)
	visit = func(c *Cell) {
		if _, ok := store[c.HashKey()]; ok {
			return
		}
		store[c.HashKey()] = cellWithLazyRefsFromCell(c, load)
		for _, ref := range c.rawRefs() {
			visit(ref)
		}
	}
	visit(root)
	return mustCreateLazyPrunedRef(tb, lazyRefFromCell(root), load)
}

func benchmarkDictReusePlain(tb testing.TB, keys []uint64) *Dictionary {
	tb.Helper()
	d := NewDict(16)
	for _, key := range keys {
		if err := d.SetBuilderByUintKey(key, BeginCell().MustStoreUInt(key+1, 16)); err != nil {
			tb.Fatal(err)
		}
	}
	return d
}

func benchmarkDictReuseAugmented(tb testing.TB, count int) *AugmentedDictionary {
	tb.Helper()
	d, err := NewAugDict(16, testMetricAugmentation{})
	if err != nil {
		tb.Fatal(err)
	}
	for key := range count {
		if _, err := d.SetBuilderByUintKeyWithMode(uint64(key), BeginCell().MustStoreUInt(uint64(key)+1, 16), DictSetModeSet); err != nil {
			tb.Fatal(err)
		}
	}
	return d
}

func BenchmarkDictionaryLazyReuse(b *testing.B) {
	chainKeys := []uint64{0}
	for bit := range 16 {
		chainKeys = append(chainKeys, uint64(1)<<bit)
	}

	for _, cache := range []bool{false, true} {
		name := "resident"
		if cache {
			name = "lazy_cache"
		}
		b.Run(name, func(b *testing.B) {
			b.Run("nearest", func(b *testing.B) {
				root := benchmarkDictReusePlain(b, []uint64{100, 101, 102, 103}).AsCell()
				var calls uint64
				if cache {
					root = benchmarkDictCachedLazyRoot(b, root, &calls)
				}
				d := root.AsDict(16)
				query := BeginCell().MustStoreUInt(99, 16).EndCell()
				b.ReportAllocs()
				for b.Loop() {
					if _, _, err := d.LookupNearestKey(query, true, true, false); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(calls)/float64(b.N), "loads/op")
			})
			b.Run("aligned_diff", func(b *testing.B) {
				left := benchmarkDictReusePlain(b, chainKeys).AsCell()
				right := benchmarkDictReusePlain(b, []uint64{0}).AsCell()
				var calls uint64
				if cache {
					left = benchmarkDictCachedLazyRoot(b, left, &calls)
					right = benchmarkDictCachedLazyRoot(b, right, &calls)
				}
				old, next := left.AsDict(16), right.AsDict(16)
				b.ReportAllocs()
				for b.Loop() {
					if err := old.ScanDiffRaw(next, func(DictDiffRawView) error { return nil }); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(calls)/float64(b.N), "loads/op")
			})
			b.Run("aligned_combine", func(b *testing.B) {
				left := benchmarkDictReusePlain(b, chainKeys).AsCell()
				right := benchmarkDictReusePlain(b, []uint64{0}).AsCell()
				var calls uint64
				if cache {
					left = benchmarkDictCachedLazyRoot(b, left, &calls)
					right = benchmarkDictCachedLazyRoot(b, right, &calls)
				}
				other := right.AsDict(16)
				b.ReportAllocs()
				for b.Loop() {
					if err := left.AsDict(16).CombineWith(other, combineUint16Values); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(calls)/float64(b.N), "loads/op")
			})
			b.Run("checked_aug_diff", func(b *testing.B) {
				next := benchmarkDictReuseAugmented(b, 256)
				old := benchmarkDictReuseAugmented(b, 0)
				var calls uint64
				if cache {
					next = benchmarkDictCachedLazyRoot(b, next.RootCell(), &calls).AsAugDict(16, testMetricAugmentation{})
				}
				b.ReportAllocs()
				for b.Loop() {
					if err := old.ScanDiffRaw(next, true, func(AugDictDiffRawView) error { return nil }); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(calls)/float64(b.N), "loads/op")
			})
		})
	}
}
