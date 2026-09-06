package cell

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"
)

func BenchmarkDictionaryMultisetReuse(b *testing.B) {
	for _, count := range []int{64, 4096} {
		items := make([]DictBulkKV, count)
		keys := make([]Hash, count)
		initial := BeginCell().MustStoreUInt(42, 64)
		changed := BeginCell().MustStoreUInt(43, 64)
		for i := range items {
			var input [8]byte
			binary.BigEndian.PutUint64(input[:], uint64(i))
			keys[i] = sha256.Sum256(input[:])
			items[i] = DictBulkKV{Key: keys[i][:], Value: initial}
		}
		base, err := NewDictFromItems(256, append([]DictBulkKV(nil), items...))
		if err != nil {
			b.Fatal(err)
		}
		for _, change := range []bool{false, true} {
			name := "same"
			if change {
				name = "changed"
			}
			b.Run(fmt.Sprintf("%s/%d", name, count), func(b *testing.B) {
				batch := append([]DictBulkKV(nil), items...)
				if change {
					for i := range batch {
						batch[i].Value = changed
					}
				}
				work := make([]DictBulkKV, count)
				b.ReportAllocs()
				for b.Loop() {
					copy(work, batch)
					d := base.Copy()
					if err := d.Multiset(work); err != nil {
						b.Fatal(err)
					}
					benchmarkCellSink = d.AsCell()
				}
			})
		}
	}
}

func BenchmarkDictionaryMultisetLazyLabelSplit(b *testing.B) {
	base := NewDict(16)
	if err := base.SetBuilderByUintKey(0, BeginCell().MustStoreUInt(1, 64)); err != nil {
		b.Fatal(err)
	}
	var calls uint64
	root := benchmarkDictCachedLazyRoot(b, base.AsCell(), &calls)
	batch := make([]DictBulkKV, 17)
	keys := make([][2]byte, len(batch))
	for i := range batch {
		var key uint16
		if i > 0 {
			key = uint16(1) << (i - 1)
		}
		binary.BigEndian.PutUint16(keys[i][:], key)
		batch[i] = DictBulkKV{Key: keys[i][:], Value: BeginCell().MustStoreUInt(uint64(key)+2, 64)}
	}
	work := make([]DictBulkKV, len(batch))
	b.ReportAllocs()
	for b.Loop() {
		copy(work, batch)
		d := root.AsDict(16)
		if err := d.Multiset(work); err != nil {
			b.Fatal(err)
		}
		benchmarkCellSink = d.AsCell()
	}
	b.ReportMetric(float64(calls)/float64(b.N), "loads/op")
}

func BenchmarkAugmentedDictionaryBulkCellValues(b *testing.B) {
	for _, count := range []int{64, 4096} {
		keys := make([]Hash, count)
		initial := make([]AugmentedEntry, count)
		entries := make([]AugmentedEntry, count)
		byteEntries := make([]AugmentedBytesEntry, count)
		uintEntries := make([]AugmentedUintEntry, count)
		for i := range entries {
			var input [8]byte
			binary.BigEndian.PutUint64(input[:], uint64(i))
			keys[i] = sha256.Sum256(input[:])
			key := BeginCell().MustStoreSlice(keys[i][:], 256).EndCell()
			before := BeginCell().MustStoreUInt(uint64(i), 64).EndCell()
			after := BeginCell().MustStoreUInt(uint64(i)+1, 64).EndCell()
			initial[i] = AugmentedEntry{Key: key, Value: before}
			entries[i] = AugmentedEntry{Key: key, Value: after}
			byteEntries[i] = AugmentedBytesEntry{Key: keys[i][:], Value: after}
			uintEntries[i] = AugmentedUintEntry{Key: uint64(i), Value: after}
		}
		base, err := NewAugDict(256, bulkSumAugmentation{})
		if err != nil {
			b.Fatal(err)
		}
		if err = base.SetMany(initial); err != nil {
			b.Fatal(err)
		}
		uintBase, err := NewAugDict(256, bulkSumAugmentation{})
		if err != nil {
			b.Fatal(err)
		}
		for i := range initial {
			if _, err = uintBase.SetBuilderByUintKeyWithMode(uint64(i), initial[i].Value.ToBuilder(), DictSetModeSet); err != nil {
				b.Fatal(err)
			}
		}
		for _, api := range []string{"cell", "bytes", "uint"} {
			b.Run(fmt.Sprintf("%s/%d", api, count), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					d := base.Copy()
					var err error
					switch api {
					case "cell":
						err = d.SetMany(entries)
					case "bytes":
						err = d.SetManyByBytes(byteEntries)
					case "uint":
						d = uintBase.Copy()
						err = d.SetManyByUint(uintEntries)
					}
					if err != nil {
						b.Fatal(err)
					}
					benchmarkCellSink = d.RootCell()
				}
			})
		}
	}
}
