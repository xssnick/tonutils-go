package cell

import (
	"fmt"
	"io"
	"testing"
	"unsafe"
)

func BenchmarkBOCLazyPayloadMetadata(b *testing.B) {
	for _, cells := range []int{1 << 16, 1 << 18} {
		b.Run(fmt.Sprintf("cells%d", cells), func(b *testing.B) {
			boc := testFlatBOC(b, cells, true)
			b.ReportAllocs()
			b.SetBytes(int64(len(boc)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				benchmarkCellSink, err = FromBOCWithOptions(boc, BOCParseOptions{Lazy: true, NoCopyPayload: true})
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(cells)*float64(unsafe.Sizeof(bocPayloadCellInfo{})), "metadata-B/op")
		})
	}
}

type benchmarkPayloadRowsLoader struct {
	records      map[Hash]testLargeBOCRecord
	payloadRows  int
	payloadCalls int
}

func (l *benchmarkPayloadRowsLoader) LoadMeta(hashes []Hash, dst []LargeBOCMetaRecord) ([]LargeBOCMetaRecord, error) {
	for _, hash := range hashes {
		dst = append(dst, l.records[hash].meta)
	}
	return dst, nil
}

func (l *benchmarkPayloadRowsLoader) LoadPayload(hashes []Hash, dst []LargeBOCPayloadRecord) ([]LargeBOCPayloadRecord, error) {
	l.payloadRows += len(hashes)
	l.payloadCalls++
	for _, hash := range hashes {
		dst = append(dst, l.records[hash].payload)
	}
	return dst, nil
}

func BenchmarkToLargeBOCEmptyPayloads(b *testing.B) {
	for _, emptyPercent := range []int{0, 25, 100} {
		b.Run(fmt.Sprintf("empty%d", emptyPercent), func(b *testing.B) {
			root := benchmarkLargeBOCEmptyPayloadRoot(4096, emptyPercent)
			loader := &benchmarkPayloadRowsLoader{records: testCellRecordsForTree(root)}
			roots := []Hash{root.HashKey()}
			opts := BOCSerializeOptions{WithIndex: true, WithCRC32C: true}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := ToLargeBOC(io.Discard, roots, opts, loader, uint64(len(loader.records)), 256); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(loader.payloadRows)/float64(b.N), "payload-rows/op")
			b.ReportMetric(float64(loader.payloadCalls)/float64(b.N), "payload-calls/op")
			b.ReportMetric(float64(len(loader.records)), "cells/op")
		})
	}
}

func benchmarkLargeBOCEmptyPayloadRoot(leaves, emptyPercent int) *Cell {
	level := make([]*Cell, leaves)
	empty := BeginCell().EndCell()
	for i := range level {
		if emptyPercent != 100 {
			level[i] = BeginCell().MustStoreUInt(uint64(i), 64).EndCell()
			continue
		}

		// Encode identity in reference topology so all cells have zero bits,
		// while the fixture still contains thousands of distinct valid cells.
		c := empty
		for bits, value := leaves-1, i; bits > 0; bits, value = bits>>1, value>>1 {
			builder := BeginCell().MustStoreRef(c)
			if value&1 != 0 {
				builder.MustStoreRef(empty)
			}
			c = builder.EndCell()
		}
		level[i] = c
	}

	var nextID uint64 = uint64(leaves)
	for len(level) > 1 {
		parents := make([]*Cell, (len(level)+3)/4)
		for i := range parents {
			builder := BeginCell()
			if emptyPercent == 0 {
				builder.MustStoreUInt(nextID, 64)
				nextID++
			}
			for _, child := range level[i*4 : min(i*4+4, len(level))] {
				builder.MustStoreRef(child)
			}
			parents[i] = builder.EndCell()
		}
		level = parents
	}
	return level[0]
}
