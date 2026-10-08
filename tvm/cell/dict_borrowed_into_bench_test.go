package cell

import "testing"

var (
	borrowedIntoBenchmarkUintSink      uint64
	borrowedIntoBenchmarkSliceSink     Slice
	borrowedIntoBenchmarkSlicePtrSink  *Slice
	borrowedIntoBenchmarkSlicePtrSink2 *Slice
)

type borrowedIntoBenchmarkAugmentation struct{}

func (borrowedIntoBenchmarkAugmentation) SkipExtra(extra *Slice) error {
	_, err := extra.LoadUInt(32)
	return err
}

func (borrowedIntoBenchmarkAugmentation) EmptyExtra(dst *Builder) error {
	return dst.StoreUInt(0, 32)
}

func (borrowedIntoBenchmarkAugmentation) LeafExtra(_ *Slice, dst *Builder) error {
	return dst.StoreUInt(1, 32)
}

func (borrowedIntoBenchmarkAugmentation) CombineExtra(left, right *Slice, dst *Builder) error {
	leftCount, err := left.LoadUInt(32)
	if err != nil {
		return err
	}
	rightCount, err := right.LoadUInt(32)
	if err != nil {
		return err
	}
	return dst.StoreUInt(leftCount+rightCount, 32)
}

func borrowedIntoBenchmarkAugDict(tb testing.TB, count int) *AugmentedDictionary {
	tb.Helper()

	dict, err := NewAugDict(16, borrowedIntoBenchmarkAugmentation{})
	if err != nil {
		tb.Fatal(err)
	}
	for i := 0; i < count; i++ {
		if err = dict.Set(benchKey(uint64(i), 16), benchKey(uint64(i^0x55), 16)); err != nil {
			tb.Fatal(err)
		}
	}
	return dict
}

func BenchmarkDictionaryLoadValueOwnedVsInto(b *testing.B) {
	dict, key := mustBuildBenchDict(b)

	b.Run("owned", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			value, err := dict.LoadValue(key)
			if err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = value.MustLoadUInt(16)
			borrowedIntoBenchmarkSlicePtrSink = value
		}
	})

	b.Run("into_reused", func(b *testing.B) {
		var value Slice
		b.ReportAllocs()
		for b.Loop() {
			if err := dict.LoadValueInto(key, &value); err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = value.MustLoadUInt(16)
			borrowedIntoBenchmarkSliceSink = value
		}
	})
}

func BenchmarkPrefixDictionaryLookupOwnedVsInto(b *testing.B) {
	dict, key := mustBuildBenchPrefixDict(b)

	b.Run("owned", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			value, matched, err := dict.LookupPrefix(key)
			if err != nil || value == nil {
				b.Fatalf("matched=%d err=%v", matched, err)
			}
			borrowedIntoBenchmarkUintSink = value.MustLoadUInt(8) + uint64(matched)
			borrowedIntoBenchmarkSlicePtrSink = value
		}
	})

	b.Run("into_reused", func(b *testing.B) {
		var value Slice
		b.ReportAllocs()
		for b.Loop() {
			matched, err := dict.LookupPrefixInto(key, &value)
			if err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = value.MustLoadUInt(8) + uint64(matched)
			borrowedIntoBenchmarkSliceSink = value
		}
	})
}

func BenchmarkAugmentedDictionaryLoadOwnedVsInto(b *testing.B) {
	dict, key, _, _ := mustBuildBenchAugDict(b)

	b.Run("raw_owned", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			valueExtra, err := dict.LoadValueWithExtra(key)
			if err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = valueExtra.MustLoadUInt(16) + valueExtra.MustLoadUInt(16)
			borrowedIntoBenchmarkSlicePtrSink = valueExtra
		}
	})

	b.Run("raw_into_reused", func(b *testing.B) {
		var valueExtra Slice
		b.ReportAllocs()
		for b.Loop() {
			if err := dict.LoadValueWithExtraInto(key, &valueExtra); err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = valueExtra.MustLoadUInt(16) + valueExtra.MustLoadUInt(16)
			borrowedIntoBenchmarkSliceSink = valueExtra
		}
	})

	b.Run("value_owned", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			value, err := dict.LoadValue(key)
			if err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = value.MustLoadUInt(16)
			borrowedIntoBenchmarkSlicePtrSink = value
		}
	})

	b.Run("value_into_reused", func(b *testing.B) {
		var value Slice
		b.ReportAllocs()
		for b.Loop() {
			if err := dict.LoadValueInto(key, &value); err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = value.MustLoadUInt(16)
			borrowedIntoBenchmarkSliceSink = value
		}
	})

	b.Run("value_extra_owned", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			value, extra, err := dict.LoadValueExtra(key)
			if err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = value.MustLoadUInt(16) + extra.MustLoadUInt(16)
			borrowedIntoBenchmarkSlicePtrSink = value
			borrowedIntoBenchmarkSlicePtrSink2 = extra
		}
	})

	b.Run("value_extra_into_reused", func(b *testing.B) {
		var value, extra Slice
		b.ReportAllocs()
		for b.Loop() {
			if err := dict.LoadValueExtraInto(key, &value, &extra); err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = value.MustLoadUInt(16) + extra.MustLoadUInt(16)
			borrowedIntoBenchmarkSliceSink = value
		}
	})
}

func BenchmarkDictionaryBorrowedFullTraversal4096(b *testing.B) {
	dict := benchmarkSequentialDict(b, 16, 4096)

	b.Run("owned_item", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			it, err := dict.Iterator(false, false)
			if err != nil {
				b.Fatal(err)
			}
			var sum uint64
			for it.Next() {
				item := it.Item()
				sum += item.Key.MustBeginParse().MustLoadUInt(16)
				sum += item.Value.MustLoadUInt(16)
			}
			if err = it.Err(); err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = sum
		}
	})

	b.Run("borrowed_view", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			it, err := dict.Iterator(false, false)
			if err != nil {
				b.Fatal(err)
			}
			var sum uint64
			for it.Next() {
				item := it.View()
				key := item.Key
				value := item.Value
				sum += key.MustLoadUInt(16)
				sum += value.MustLoadUInt(16)
			}
			if err = it.Err(); err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = sum
		}
	})

	b.Run("borrowed_foreach", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var sum uint64
			err := dict.ForEachBorrowed(false, false, func(item DictItemView) error {
				key := item.Key
				value := item.Value
				sum += key.MustLoadUInt(16)
				sum += value.MustLoadUInt(16)
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = sum
		}
	})
}

func BenchmarkAugmentedDictionaryBorrowedFullTraversal4096(b *testing.B) {
	dict := borrowedIntoBenchmarkAugDict(b, 4096)

	b.Run("owned_item", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			it, err := dict.IteratorExtra(false, false)
			if err != nil {
				b.Fatal(err)
			}
			var sum uint64
			for it.Next() {
				item := it.Item()
				sum += item.Key.MustBeginParse().MustLoadUInt(16)
				sum += item.Value.MustLoadUInt(16)
				sum += item.Extra.MustLoadUInt(32)
			}
			if err = it.Err(); err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = sum
		}
	})

	b.Run("borrowed_view", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			it, err := dict.IteratorExtra(false, false)
			if err != nil {
				b.Fatal(err)
			}
			var sum uint64
			for it.Next() {
				item := it.View()
				key := item.Key
				value := item.Value
				extra := item.Extra
				sum += key.MustLoadUInt(16)
				sum += value.MustLoadUInt(16)
				sum += extra.MustLoadUInt(32)
			}
			if err = it.Err(); err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = sum
		}
	})

	b.Run("borrowed_foreach", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var sum uint64
			err := dict.ForEachBorrowed(false, false, func(item AugDictItemView) error {
				key := item.Key
				value := item.Value
				extra := item.Extra
				sum += key.MustLoadUInt(16)
				sum += value.MustLoadUInt(16)
				sum += extra.MustLoadUInt(32)
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			borrowedIntoBenchmarkUintSink = sum
		}
	})
}
