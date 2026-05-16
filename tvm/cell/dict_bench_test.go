package cell

import "testing"

func benchKey(value uint64, bits uint) *Cell {
	return BeginCell().MustStoreUInt(value, bits).EndCell()
}

func mustBuildBenchDict(tb testing.TB) (*Dictionary, *Cell) {
	tb.Helper()

	dict := NewDict(16)
	for i := 0; i < 512; i++ {
		if err := dict.Set(
			benchKey(uint64(i), 16),
			BeginCell().MustStoreUInt(uint64(i^0x55), 16).EndCell(),
		); err != nil {
			tb.Fatal(err)
		}
	}
	return dict, benchKey(511, 16)
}

func mustBuildBenchAugDict(tb testing.TB) (*AugmentedDictionary, *Cell, *Cell, *Cell) {
	tb.Helper()

	dict, err := NewAugDict(16, testMetricAugmentation{})
	if err != nil {
		tb.Fatal(err)
	}

	for i := 0; i < 512; i++ {
		if err = dict.Set(
			benchKey(uint64(i), 16),
			BeginCell().MustStoreUInt(uint64(i^0x33), 16).EndCell(),
		); err != nil {
			tb.Fatal(err)
		}
	}

	key := benchKey(511, 16)
	return dict, key,
		BeginCell().MustStoreUInt(0x1234, 16).EndCell(),
		BeginCell().MustStoreUInt(0x5678, 16).EndCell()
}

func mustBuildBenchPrefixDict(tb testing.TB) (*PrefixDictionary, *Cell) {
	tb.Helper()

	dict := NewPrefixDict(16)
	keys := []struct {
		keyBits uint64
		keyLen  uint
		value   uint64
	}{
		{0b10, 2, 0x10},
		{0b011, 3, 0x11},
		{0b1110, 4, 0x12},
		{0b11110, 5, 0x13},
	}

	for _, item := range keys {
		if err := dict.Set(
			benchKey(item.keyBits, item.keyLen),
			BeginCell().MustStoreUInt(item.value, 8).EndCell(),
		); err != nil {
			tb.Fatal(err)
		}
	}

	return dict, benchKey(0b111101011001, 12)
}

func BenchmarkDictionaryLoadValue(b *testing.B) {
	dict, key := mustBuildBenchDict(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := dict.LoadValue(key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAugmentedDictionaryLoadValue(b *testing.B) {
	dict, key, _, _ := mustBuildBenchAugDict(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := dict.LoadValue(key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAugmentedDictionarySetReplace(b *testing.B) {
	dict, key, valueA, valueB := mustBuildBenchAugDict(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value := valueA
		if i&1 == 1 {
			value = valueB
		}

		changed, err := dict.SetWithMode(key, value, DictSetModeReplace)
		if err != nil {
			b.Fatal(err)
		}
		if !changed {
			b.Fatal("replace should always update an existing key")
		}
	}
}

func BenchmarkPrefixDictionaryLookupPrefix(b *testing.B) {
	dict, key := mustBuildBenchPrefixDict(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value, matched, err := dict.LookupPrefix(key)
		if err != nil {
			b.Fatal(err)
		}
		if value == nil || matched == 0 {
			b.Fatal("expected prefix match")
		}
	}
}
