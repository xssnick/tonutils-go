package cell

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"
)

func BenchmarkDictionaryBulkScratch(b *testing.B) {
	for _, count := range []int{64, 4096} {
		items := make([]DictBulkKV, count)
		keys := make([]Hash, count)
		value := BeginCell().MustStoreUInt(42, 64)
		for i := range items {
			var input [8]byte
			binary.BigEndian.PutUint64(input[:], uint64(i))
			keys[i] = sha256.Sum256(input[:])
			items[i] = DictBulkKV{Key: keys[i][:], Value: value}
		}
		base, err := NewDictFromItems(256, append([]DictBulkKV(nil), items...))
		if err != nil {
			b.Fatal(err)
		}
		work := make([]DictBulkKV, count)
		b.Run(fmt.Sprintf("Build%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				copy(work, items)
				d, err := NewDictFromItems(256, work)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkCellSink = d.AsCell()
			}
		})
		b.Run(fmt.Sprintf("Multiset%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				copy(work, items)
				d := base.Copy()
				if err := d.Multiset(work); err != nil {
					b.Fatal(err)
				}
				benchmarkCellSink = d.AsCell()
			}
		})
	}
}

func BenchmarkSliceToCellFullTraced(b *testing.B) {
	childTrace := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	trace := NewTrace(TraceHooks{
		OnCreate: func() {},
		OnChild:  func(int) *Trace { return childTrace },
	})
	leaf := BeginCell().MustStoreUInt(1, 8).EndCell()
	root := BeginCell().MustStoreUInt(42, 64).MustStoreRef(leaf).MustStoreRef(leaf).EndCell()
	s, err := root.BeginParseWithTrace(trace)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		cl, err := s.ToCell()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkCellSink = cl
	}
}
