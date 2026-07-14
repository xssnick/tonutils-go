package cell

import (
	"math/big"
	"testing"
)

var (
	benchmarkDictCellSink  *Cell
	benchmarkDictSliceSink *Slice
	benchmarkDictIntSink   int
)

func BenchmarkDictIteratorTraversal(b *testing.B) {
	dict := benchmarkSequentialDict(b, 16, 4096)

	b.Run("early", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			it, err := dict.Iterator(false, false)
			if err != nil || !it.Next() {
				b.Fatal(err)
			}
			benchmarkDictCellSink = it.Key()
		}
	})

	b.Run("full", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			it, err := dict.Iterator(false, false)
			if err != nil {
				b.Fatal(err)
			}
			count := 0
			for it.Next() {
				benchmarkDictCellSink = it.Key()
				count++
			}
			if err = it.Err(); err != nil {
				b.Fatal(err)
			}
			benchmarkDictIntSink = count
		}
	})
}

func BenchmarkDictLookupNearestWide(b *testing.B) {
	for _, bits := range []uint{256, 1023} {
		b.Run(big.NewInt(int64(bits)).String(), func(b *testing.B) {
			dict := NewDict(bits)
			for i := uint(0); i < 64; i++ {
				if err := dict.Set(wideTransitionKey(i, bits), BeginCell().MustStoreUInt(uint64(i), 8).EndCell()); err != nil {
					b.Fatal(err)
				}
			}
			query := wideTransitionKey(48, bits)
			b.ReportAllocs()
			for b.Loop() {
				key, value, err := dict.LookupNearestKey(query, true, true, false)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkDictCellSink = key
				benchmarkDictSliceSink = value
			}
		})
	}
}

func BenchmarkDictionaryFilterSparse(b *testing.B) {
	dict := benchmarkSequentialDict(b, 16, 4096)
	b.ReportAllocs()
	for b.Loop() {
		filtered := dict.Copy()
		changes, err := filtered.Filter(func(_ *Slice, key *Cell) (DictFilterAction, error) {
			if key.MustBeginParse().MustLoadUInt(16) == 2048 {
				return DictFilterRemove, nil
			}
			return DictFilterKeep, nil
		})
		if err != nil || changes != 1 {
			b.Fatalf("changes=%d err=%v", changes, err)
		}
		benchmarkDictCellSink = filtered.root
	}
}

func BenchmarkBuilderStoreSnake(b *testing.B) {
	for _, size := range []int{64, 4096} {
		b.Run(big.NewInt(int64(size)).String(), func(b *testing.B) {
			data := make([]byte, size)
			b.ReportAllocs()
			for b.Loop() {
				var builder Builder
				if err := builder.StoreBinarySnake(data); err != nil {
					b.Fatal(err)
				}
				benchmarkDictIntSink = builder.RefsUsed()
			}
		})
	}
}

func BenchmarkDictionaryLoadValueByIntKey(b *testing.B) {
	dict := benchmarkSequentialDict(b, 64, 1024)
	key := big.NewInt(777)
	b.ReportAllocs()
	for b.Loop() {
		value, err := dict.LoadValueByIntKey(key)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkDictSliceSink = value
	}
}

func BenchmarkDictionaryLoadMinMaxAndDelete(b *testing.B) {
	dict := benchmarkSequentialDict(b, 16, 4096)
	b.ReportAllocs()
	for b.Loop() {
		copy := dict.Copy()
		key, value, err := copy.LoadMinMaxAndDelete(false, false)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkDictCellSink = key
		benchmarkDictSliceSink = value
	}
}

func benchmarkSequentialDict(tb testing.TB, bits uint, count int) *Dictionary {
	tb.Helper()
	dict := NewDict(bits)
	for i := 0; i < count; i++ {
		key := BeginCell().MustStoreUInt(uint64(i), bits).EndCell()
		if err := dict.Set(key, BeginCell().MustStoreUInt(uint64(i), bits).EndCell()); err != nil {
			tb.Fatal(err)
		}
	}
	return dict
}
