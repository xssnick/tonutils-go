package cell

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
)

type writabilityProbeAugmentation struct {
	testMetricAugmentation
	emptyCalls int
	emptyErr   error
}

func (a *writabilityProbeAugmentation) EmptyExtra(dst *Builder) error {
	a.emptyCalls++
	if a.emptyErr != nil {
		return a.emptyErr
	}
	return a.testMetricAugmentation.EmptyExtra(dst)
}

func TestAugmentedDictionaryRawKeyParity(t *testing.T) {
	for _, keyBits := range []uint{1, 7, 8, 9, 64, 256, 352} {
		t.Run(testNameBits(keyBits), func(t *testing.T) {
			rnd := rand.New(rand.NewSource(int64(2026082500 + keyBits)))
			byCell := mustNewRawKeyTestAugDict(t, keyBits)
			byBytes := mustNewRawKeyTestAugDict(t, keyBits)

			count := 64
			if keyBits < 7 {
				count = 1 << keyBits
			}
			keys := uniqueRawAugKeys(rnd, keyBits, count)
			for i, key := range keys {
				value := rawKeyTestValue(uint64(i + 1))
				if err := byCell.Set(rawKeyTestCell(t, key, keyBits), value); err != nil {
					t.Fatalf("cell Set %d: %v", i, err)
				}
				var valueBuilder Builder
				value.ToBuilderInto(&valueBuilder)
				if err := byBytes.SetBuilderByBytesKey(key, &valueBuilder); err != nil {
					t.Fatalf("bytes Set %d: %v", i, err)
				}
			}
			assertRawKeyDictEqual(t, byBytes, byCell)

			for i, key := range keys {
				var value, extra Slice
				if err := byBytes.LoadValueExtraByBytesKeyInto(key, &value, &extra); err != nil {
					t.Fatalf("bytes lookup %d: %v", i, err)
				}
				if got := value.MustLoadUInt(32); got != uint64(i+1) {
					t.Fatalf("bytes lookup %d value = %d", i, got)
				}
				if got := extra.MustLoadUInt(16); got != 32 {
					t.Fatalf("bytes lookup %d extra = %d", i, got)
				}

				keySource := BeginCell().MustStoreUInt(5, 3).MustStoreSlice(key, keyBits).MustStoreUInt(2, 2).EndCell().MustBeginParse()
				if err := keySource.SkipBits(3); err != nil {
					t.Fatal(err)
				}
				bitsBefore := keySource.BitsLeft()
				var sliceValue Slice
				if err := byBytes.LoadValueBySliceKeyInto(keySource, &sliceValue); err != nil {
					t.Fatalf("slice lookup %d: %v", i, err)
				}
				if keySource.BitsLeft() != bitsBefore {
					t.Fatalf("slice lookup %d consumed its key", i)
				}
				if got := sliceValue.MustLoadUInt(32); got != uint64(i+1) {
					t.Fatalf("slice lookup %d value = %d", i, got)
				}
			}

			for i := 0; i < len(keys); i += 3 {
				if err := byCell.Delete(rawKeyTestCell(t, keys[i], keyBits)); err != nil {
					t.Fatalf("cell Delete %d: %v", i, err)
				}
				if err := byBytes.DeleteByBytesKey(keys[i]); err != nil {
					t.Fatalf("bytes Delete %d: %v", i, err)
				}
			}
			assertRawKeyDictEqual(t, byBytes, byCell)
		})
	}
}

func TestAugmentedDictionaryRawUintKeyParity(t *testing.T) {
	for _, keyBits := range []uint{4, 64, 65, 256, 352} {
		t.Run(testNameBits(keyBits), func(t *testing.T) {
			byCell := mustNewRawKeyTestAugDict(t, keyBits)
			byUint := mustNewRawKeyTestAugDict(t, keyBits)
			values := []uint64{0, 1, 3, 9}
			if keyBits >= 64 {
				values = append(values, ^uint64(0), uint64(1)<<63)
			}

			for i, key := range values {
				value := rawKeyTestValue(uint64(100 + i))
				var keyBuilder Builder
				if err := initUintKeyBuilder(key, keyBits, &keyBuilder); err != nil {
					t.Fatal(err)
				}
				keyCell := keyBuilder.EndCell()
				if err := byCell.Set(keyCell, value); err != nil {
					t.Fatal(err)
				}
				var valueBuilder Builder
				value.ToBuilderInto(&valueBuilder)
				if _, err := byUint.SetBuilderByUintKeyWithMode(key, &valueBuilder, DictSetModeSet); err != nil {
					t.Fatal(err)
				}
			}
			assertRawKeyDictEqual(t, byUint, byCell)

			for i, key := range values {
				var value, extra Slice
				if err := byUint.LoadValueExtraByUintKeyInto(key, &value, &extra); err != nil {
					t.Fatal(err)
				}
				if got := value.MustLoadUInt(32); got != uint64(100+i) {
					t.Fatalf("uint value = %d", got)
				}
				if got := extra.MustLoadUInt(16); got != 32 {
					t.Fatalf("uint extra = %d", got)
				}
			}
		})
	}
}

func TestAugmentedDictionaryRawKeyLoadDelete(t *testing.T) {
	const keyBits = 64
	const key = uint64(0x1234567890abcdef)
	base := mustNewRawKeyTestAugDict(t, keyBits)
	if _, err := base.SetBuilderByUintKeyWithMode(key, rawKeyTestValue(key&0xffffffff).ToBuilder(), DictSetModeSet); err != nil {
		t.Fatal(err)
	}
	keyCell := BeginCell().MustStoreUInt(key, keyBits).EndCell()
	keyBytes := keyCell.MustBeginParse().MustLoadSlice(keyBits)

	byBytes := base.Copy()
	var value, extra Slice
	if err := byBytes.LoadValueExtraAndDeleteByBytesKeyInto(keyBytes, &value, &extra); err != nil {
		t.Fatal(err)
	}
	if got := value.MustLoadUInt(32); got != key&0xffffffff {
		t.Fatalf("bytes deleted value = %d", got)
	}
	if got := extra.MustLoadUInt(16); got != 32 {
		t.Fatalf("bytes deleted extra = %d", got)
	}
	if !byBytes.IsEmpty() {
		t.Fatal("bytes load-delete did not empty dictionary")
	}

	bySlice := base.Copy()
	keySlice := keyCell.MustBeginParse()
	if err := bySlice.LoadValueAndDeleteBySliceKeyInto(keySlice, &value); err != nil {
		t.Fatal(err)
	}
	if !bySlice.IsEmpty() || keySlice.BitsLeft() != keyBits {
		t.Fatal("slice load-delete changed key or kept dictionary entry")
	}

	byUint := base.Copy()
	if err := byUint.LoadValueAndDeleteByUintKeyInto(key, &value); err != nil {
		t.Fatal(err)
	}
	if !byUint.IsEmpty() {
		t.Fatal("uint load-delete did not empty dictionary")
	}
	if err := byUint.LoadValueAndDeleteByUintKeyInto(key, &value); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("missing uint load-delete error = %v", err)
	}
}

func TestAugmentedDictionaryDeleteManyByBytesParity(t *testing.T) {
	const keyBits = 352
	rnd := rand.New(rand.NewSource(2026082511))
	keys := uniqueRawAugKeys(rnd, keyBits, 384)
	entries := make([]AugmentedEntry, len(keys))
	keyCells := make([]*Cell, len(keys))
	for i, key := range keys {
		keyCells[i] = rawKeyTestCell(t, key, keyBits)
		entries[i] = AugmentedEntry{Key: keyCells[i], Value: rawKeyTestValue(uint64(i + 1))}
	}
	base := mustNewRawKeyTestAugDict(t, keyBits)
	if err := base.SetMany(entries); err != nil {
		t.Fatal(err)
	}

	deletedBytes := append([][]byte(nil), keys[:257]...)
	deletedCells := append([]*Cell(nil), keyCells[:257]...)
	rnd.Shuffle(len(deletedBytes), func(i, j int) {
		deletedBytes[i], deletedBytes[j] = deletedBytes[j], deletedBytes[i]
		deletedCells[i], deletedCells[j] = deletedCells[j], deletedCells[i]
	})

	byCell := base.Copy()
	if err := byCell.DeleteMany(deletedCells); err != nil {
		t.Fatal(err)
	}
	for _, parallelism := range []int{1, 8} {
		byBytes := base.Copy()
		if err := byBytes.DeleteManyByBytes(deletedBytes, parallelism); err != nil {
			t.Fatalf("parallelism %d: %v", parallelism, err)
		}
		assertRawKeyDictEqual(t, byBytes, byCell)
	}
}

func TestAugmentedDictionarySetManyRawKeyParity(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		const keyBits = 352
		rnd := rand.New(rand.NewSource(2026082513))
		keys := uniqueRawAugKeys(rnd, keyBits, 257)
		cellEntries := make([]AugmentedEntry, len(keys))
		bytesEntries := make([]AugmentedBytesEntry, len(keys))
		for i, key := range keys {
			value := rawKeyTestValue(uint64(i + 1))
			cellEntries[i] = AugmentedEntry{Key: rawKeyTestCell(t, key, keyBits), Value: value}
			bytesEntries[i] = AugmentedBytesEntry{Key: key, Value: value}
		}

		byCell := mustNewRawKeyTestAugDict(t, keyBits)
		if err := byCell.SetMany(cellEntries); err != nil {
			t.Fatal(err)
		}
		for _, parallelism := range []int{1, 8} {
			byBytes := mustNewRawKeyTestAugDict(t, keyBits)
			if err := byBytes.SetManyByBytes(bytesEntries, parallelism); err != nil {
				t.Fatalf("parallelism %d: %v", parallelism, err)
			}
			assertRawKeyDictEqual(t, byBytes, byCell)
		}
	})

	t.Run("uint", func(t *testing.T) {
		const keyBits = 256
		cellEntries := make([]AugmentedEntry, 257)
		uintEntries := make([]AugmentedUintEntry, len(cellEntries))
		for i := range cellEntries {
			key := uint64(i*17 + 3)
			var keyBuilder Builder
			if err := initUintKeyBuilder(key, keyBits, &keyBuilder); err != nil {
				t.Fatal(err)
			}
			value := rawKeyTestValue(uint64(i + 1))
			cellEntries[i] = AugmentedEntry{Key: keyBuilder.EndCell(), Value: value}
			uintEntries[i] = AugmentedUintEntry{Key: key, Value: value}
		}

		byCell := mustNewRawKeyTestAugDict(t, keyBits)
		if err := byCell.SetMany(cellEntries); err != nil {
			t.Fatal(err)
		}
		for _, parallelism := range []int{1, 8} {
			byUint := mustNewRawKeyTestAugDict(t, keyBits)
			if err := byUint.SetManyByUint(uintEntries, parallelism); err != nil {
				t.Fatalf("parallelism %d: %v", parallelism, err)
			}
			assertRawKeyDictEqual(t, byUint, byCell)
		}
	})
}

func TestAugmentedDictionaryRawKeyReferencedValueParity(t *testing.T) {
	const keyBits = 352
	keys := [][]byte{
		make([]byte, keyBits/8),
		append([]byte{0x80}, make([]byte, keyBits/8-1)...),
		append([]byte{0x40}, make([]byte, keyBits/8-1)...),
	}
	values := make([]*Cell, len(keys))
	for i := range values {
		payload := BeginCell().MustStoreUInt(uint64(0xa0+i), 8).EndCell()
		values[i] = BeginCell().MustStoreUInt(uint64(i), 3).MustStoreRef(payload).EndCell()
	}

	newDict := func() *AugmentedDictionary {
		dict, err := NewAugDict(keyBits, &diffReferencedValueAugmentation{})
		if err != nil {
			t.Fatal(err)
		}
		return dict
	}

	byCell := newDict()
	cellEntries := make([]AugmentedEntry, len(keys))
	for i := range keys {
		cellEntries[i] = AugmentedEntry{
			Key:   rawKeyTestCell(t, keys[i], keyBits),
			Value: values[i],
			Mode:  DictSetModeAdd,
		}
	}
	if err := byCell.SetMany(cellEntries); err != nil {
		t.Fatal(err)
	}

	for _, parallelism := range []int{1, 8} {
		byBytes := newDict()
		entries := make([]AugmentedBytesEntry, len(keys))
		for i := range entries {
			entries[i] = AugmentedBytesEntry{
				Key:   keys[i],
				Value: values[i],
				Mode:  DictSetModeAdd,
			}
		}
		if err := byBytes.SetManyByBytes(entries, parallelism); err != nil {
			t.Fatalf("parallelism %d: %v", parallelism, err)
		}
		assertRawKeyDictEqual(t, byBytes, byCell)

		for i := range keys {
			var value Slice
			if err := byBytes.LoadValueByBytesKeyInto(keys[i], &value); err != nil {
				t.Fatal(err)
			}
			if got := value.MustLoadUInt(3); got != uint64(i) {
				t.Fatalf("value %d prefix = %d", i, got)
			}
			ref, err := value.LoadRefCell()
			if err != nil {
				t.Fatalf("value %d lost reference: %v", i, err)
			}
			if got := ref.MustBeginParse().MustLoadUInt(8); got != uint64(0xa0+i) {
				t.Fatalf("value %d referenced payload = %x", i, got)
			}
		}
	}

	byBytes := newDict()
	for i := range keys {
		var value Builder
		values[i].ToBuilderInto(&value)
		inserted, err := byBytes.SetBuilderByBytesKeyWithMode(keys[i], &value, DictSetModeAdd)
		if err != nil || !inserted {
			t.Fatalf("single set %d: inserted=%v err=%v", i, inserted, err)
		}
	}
	assertRawKeyDictEqual(t, byBytes, byCell)
}

func TestAugmentedDictionaryRechecksWritability(t *testing.T) {
	aug := &writabilityProbeAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	if aug.emptyCalls != 1 {
		t.Fatalf("constructor EmptyExtra calls = %d", aug.emptyCalls)
	}

	if _, err = dict.SetBuilderByUintKeyWithMode(1, rawKeyTestValue(1).ToBuilder(), DictSetModeAdd); err != nil {
		t.Fatal(err)
	}
	dynamicErr := errors.New("dynamic augmentation failure")
	aug.emptyErr = dynamicErr
	if _, err = dict.SetBuilderByUintKeyWithMode(2, rawKeyTestValue(2).ToBuilder(), DictSetModeAdd); err != dynamicErr {
		t.Fatalf("dynamic writability error = %v", err)
	}
	aug.emptyErr = nil
	if _, err = dict.SetBuilderByUintKeyWithMode(2, rawKeyTestValue(2).ToBuilder(), DictSetModeAdd); err != nil {
		t.Fatal(err)
	}

	copyDict := dict.Copy()
	if _, err = copyDict.SetBuilderByUintKeyWithMode(3, rawKeyTestValue(3).ToBuilder(), DictSetModeAdd); err != nil {
		t.Fatal(err)
	}
	if aug.emptyCalls != 5 {
		t.Fatalf("EmptyExtra calls = %d, want one constructor and four mutation checks", aug.emptyCalls)
	}

	readOnlyErr := fmt.Errorf("wrapped: %w", ErrAugmentationSemanticsUnavailable)
	readOnly := &writabilityProbeAugmentation{emptyErr: readOnlyErr}
	readOnlyDict := &AugmentedDictionary{keySz: 8, aug: readOnly}
	for range 2 {
		_, err = readOnlyDict.SetBuilderByUintKeyWithMode(1, BeginCell(), DictSetModeSet)
		if !errors.Is(err, ErrAugmentationSemanticsUnavailable) || err != readOnlyErr {
			t.Fatalf("read-only mutation error = %v", err)
		}
	}
	if readOnly.emptyCalls != 2 {
		t.Fatalf("read-only EmptyExtra calls = %d", readOnly.emptyCalls)
	}
}

func TestAugmentedDictionaryRawBulkModes(t *testing.T) {
	const keyBits = 8
	base := mustNewRawKeyTestAugDict(t, keyBits)
	if err := base.Set(rawKeyTestCell(t, []byte{0x10}, keyBits), rawKeyTestValue(1)); err != nil {
		t.Fatal(err)
	}

	cellEntries := []AugmentedEntry{
		{Key: rawKeyTestCell(t, []byte{0x10}, keyBits), Value: rawKeyTestValue(2), Mode: DictSetModeReplace},
		{Key: rawKeyTestCell(t, []byte{0x20}, keyBits), Value: rawKeyTestValue(3), Mode: DictSetModeAdd},
	}
	bytesEntries := []AugmentedBytesEntry{
		{Key: []byte{0x10}, Value: rawKeyTestValue(2), Mode: DictSetModeReplace},
		{Key: []byte{0x20}, Value: rawKeyTestValue(3), Mode: DictSetModeAdd},
	}
	byCell := base.Copy()
	if err := byCell.SetMany(cellEntries); err != nil {
		t.Fatal(err)
	}
	for _, parallelism := range []int{1, 8} {
		byBytes := base.Copy()
		if err := byBytes.SetManyByBytes(bytesEntries, parallelism); err != nil {
			t.Fatalf("parallelism %d: %v", parallelism, err)
		}
		assertRawKeyDictEqual(t, byBytes, byCell)
	}

	for _, tc := range []struct {
		name  string
		entry AugmentedBytesEntry
	}{
		{
			name:  "add existing",
			entry: AugmentedBytesEntry{Key: []byte{0x10}, Value: rawKeyTestValue(4), Mode: DictSetModeAdd},
		},
		{
			name:  "replace absent",
			entry: AugmentedBytesEntry{Key: []byte{0x30}, Value: rawKeyTestValue(4), Mode: DictSetModeReplace},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dict := base.Copy()
			before := dict.AsCell().HashKey()
			if err := dict.SetManyByBytes([]AugmentedBytesEntry{tc.entry}); err == nil {
				t.Fatal("mode mismatch was accepted")
			}
			if got := dict.AsCell().HashKey(); got != before {
				t.Fatal("failed mode assertion mutated dictionary")
			}
		})
	}
}

func TestFixedDictNodeSplitLabelNoAlloc(t *testing.T) {
	label := BeginCell().MustStoreUInt(0xb3, 8).EndCell().MustBeginParse()
	node := fixedDictNode{label: *label, labelLen: 8}
	var prefix, remainder Slice
	allocs := testing.AllocsPerRun(1000, func() {
		var err error
		prefix, remainder, err = node.splitLabel(3)
		if err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("splitLabel allocations = %v", allocs)
	}
	if got := prefix.MustLoadUInt(3); got != 5 {
		t.Fatalf("prefix = %b", got)
	}
	if got := remainder.MustLoadUInt(4); got != 3 {
		t.Fatalf("remainder = %b", got)
	}
}

func TestAugmentedDictionaryRawLookupNoAlloc(t *testing.T) {
	const keyBits = 352
	key := make([]byte, keyBits/8)
	key[0] = 0xa5
	dict := mustNewRawKeyTestAugDict(t, keyBits)
	if err := dict.SetBuilderByBytesKey(key, rawKeyTestValue(7).ToBuilder()); err != nil {
		t.Fatal(err)
	}

	var value Slice
	allocs := testing.AllocsPerRun(1000, func() {
		if err := dict.LoadValueByBytesKeyInto(key, &value); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("raw lookup allocations = %v", allocs)
	}
}

func TestAugmentedDictionaryRawKeyValidationAndMasking(t *testing.T) {
	dict := mustNewRawKeyTestAugDict(t, 7)
	if err := dict.SetBuilderByBytesKey([]byte{0xaa}, rawKeyTestValue(1).ToBuilder()); err != nil {
		t.Fatal(err)
	}
	var value Slice
	if err := dict.LoadValueByBytesKeyInto([]byte{0xab}, &value); err != nil {
		t.Fatalf("unused trailing bit changed key: %v", err)
	}
	if got := value.MustLoadUInt(32); got != 1 {
		t.Fatalf("masked lookup value = %d", got)
	}

	before := dict.AsCell().HashKey()
	if err := dict.SetManyByBytes([]AugmentedBytesEntry{
		{Key: []byte{0xaa}, Value: rawKeyTestValue(2)},
		{Key: []byte{0xab}, Value: rawKeyTestValue(3)},
	}); err == nil {
		t.Fatal("duplicate masked bulk set keys were accepted")
	}
	if got := dict.AsCell().HashKey(); got != before {
		t.Fatal("failed duplicate bulk set mutated dictionary")
	}
	if err := dict.DeleteManyByBytes([][]byte{{0xaa}, {0xab}}); err == nil {
		t.Fatal("duplicate masked keys were accepted")
	}
	if got := dict.AsCell().HashKey(); got != before {
		t.Fatal("failed duplicate batch mutated dictionary")
	}
	if err := dict.DeleteManyByBytes([][]byte{{}}); err == nil {
		t.Fatal("short batch key was accepted")
	}
	if err := dict.LoadValueByBytesKeyInto(nil, &value); err == nil {
		t.Fatal("short lookup key was accepted")
	}
	if _, err := dict.SetBuilderByBytesKeyWithMode(nil, BeginCell(), DictSetModeSet); err == nil {
		t.Fatal("short set key was accepted")
	}
	shortSlice := BeginCell().MustStoreUInt(0, 6).EndCell().MustBeginParse()
	if err := dict.LoadValueBySliceKeyInto(shortSlice, &value); err == nil {
		t.Fatal("short slice key was accepted")
	}

	narrow := mustNewRawKeyTestAugDict(t, 4)
	if err := narrow.LoadValueByUintKeyInto(16, &value); !errors.Is(err, ErrTooBigValue) {
		t.Fatalf("overflowing uint lookup error = %v", err)
	}
	if err := narrow.SetManyByUint([]AugmentedUintEntry{{Key: 16, Value: rawKeyTestValue(1)}}); !errors.Is(err, ErrTooBigValue) {
		t.Fatalf("overflowing uint bulk set error = %v", err)
	}
}

func BenchmarkAugmentedDictionaryRawBytesLookup(b *testing.B) {
	const keyBits = 352
	rnd := rand.New(rand.NewSource(2026082512))
	keys := uniqueRawAugKeys(rnd, keyBits, 1024)
	dict := mustNewRawKeyTestAugDict(b, keyBits)
	keyCells := make([]*Cell, len(keys))
	for i, key := range keys {
		keyCells[i] = rawKeyTestCell(b, key, keyBits)
		if err := dict.Set(keyCells[i], rawKeyTestValue(uint64(i))); err != nil {
			b.Fatal(err)
		}
	}

	b.Run("cell-owned", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			if _, err := dict.LoadValue(keyCells[i%len(keyCells)]); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("bytes-into", func(b *testing.B) {
		b.ReportAllocs()
		var value Slice
		for i := 0; b.Loop(); i++ {
			if err := dict.LoadValueByBytesKeyInto(keys[i%len(keys)], &value); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkAugmentedDictionaryEnsureWritable(b *testing.B) {
	dict := &AugmentedDictionary{aug: testMetricAugmentation{}}
	b.ReportAllocs()
	for b.Loop() {
		if err := dict.ensureWritable(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAugmentedDictionarySetManyRawBytes(b *testing.B) {
	const keyBits = 352
	rnd := rand.New(rand.NewSource(2026082514))
	keys := uniqueRawAugKeys(rnd, keyBits, 256)
	bytesEntries := make([]AugmentedBytesEntry, len(keys))
	for i, key := range keys {
		bytesEntries[i] = AugmentedBytesEntry{Key: key, Value: rawKeyTestValue(uint64(i + 1))}
	}
	base := mustNewRawKeyTestAugDict(b, keyBits)

	b.Run("cell-materialized", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			entries := make([]AugmentedEntry, len(keys))
			for i, key := range keys {
				entries[i] = AugmentedEntry{Key: rawKeyTestCell(b, key, keyBits), Value: bytesEntries[i].Value}
			}
			dict := base.Copy()
			if err := dict.SetMany(entries); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("bytes", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			dict := base.Copy()
			if err := dict.SetManyByBytes(bytesEntries); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func mustNewRawKeyTestAugDict(tb testing.TB, keyBits uint) *AugmentedDictionary {
	tb.Helper()
	dict, err := NewAugDict(keyBits, testMetricAugmentation{})
	if err != nil {
		tb.Fatal(err)
	}
	return dict
}

func rawKeyTestCell(tb testing.TB, key []byte, bits uint) *Cell {
	tb.Helper()
	cell := BeginCell()
	if err := cell.StoreSlice(key, bits); err != nil {
		tb.Fatal(err)
	}
	return cell.EndCell()
}

func rawKeyTestValue(value uint64) *Cell {
	return BeginCell().MustStoreUInt(value, 32).EndCell()
}

func uniqueRawAugKeys(rnd *rand.Rand, bits uint, count int) [][]byte {
	keys := make([][]byte, 0, count)
	seen := make(map[string]struct{}, count)
	for len(keys) < count {
		key := make([]byte, (bits+7)/8)
		_, _ = rnd.Read(key)
		canonical := append([]byte(nil), key...)
		if rem := bits % 8; rem != 0 {
			canonical[len(canonical)-1] &= 0xff << (8 - rem)
		}
		id := string(canonical)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func assertRawKeyDictEqual(t *testing.T, got, want *AugmentedDictionary) {
	t.Helper()
	if gotHash, wantHash := got.AsCell().HashKey(), want.AsCell().HashKey(); gotHash != wantHash {
		t.Fatalf("dictionary hash = %x, want %x", gotHash, wantHash)
	}
}

func testNameBits(bits uint) string {
	const digits = "0123456789"
	if bits < 10 {
		return "bits=" + string(digits[bits])
	}
	var buf [3]byte
	i := len(buf)
	for bits != 0 {
		i--
		buf[i] = byte('0' + bits%10)
		bits /= 10
	}
	return "bits=" + string(buf[i:])
}
