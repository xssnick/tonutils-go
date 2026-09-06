package cell

import (
	"fmt"
	"testing"
)

func BenchmarkBOCLazyMaterializeRefCounts(b *testing.B) {
	for refs := 0; refs <= 4; refs++ {
		b.Run(fmt.Sprintf("refs%d", refs), func(b *testing.B) {
			builder := BeginCell().MustStoreUInt(0xAA, 8)
			for i := 0; i < refs; i++ {
				builder.MustStoreRef(BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
			}
			child := builder.EndCell()
			root := BeginCell().MustStoreRef(child).EndCell()
			boc := root.ToBOCWithOptions(BOCSerializeOptions{WithIndex: true, WithCacheBits: true})
			parsed, err := FromBOCWithOptions(boc, BOCParseOptions{Lazy: true, NoCopyPayload: true})
			if err != nil {
				b.Fatal(err)
			}
			boundary := parsed.MustPeekRef(0)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkCellSink, err = boundary.Prewarm()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPrewarmRecursiveSharedLazy(b *testing.B) {
	leaf := BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).MustStoreRef(leaf).MustStoreRef(leaf).MustStoreRef(leaf).EndCell()
	var calls int
	lazy := cellWithLazyRefsFromCell(root, func(Hash) (*Cell, error) {
		calls++
		return leaf, nil
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		benchmarkCellSink, err = lazy.PrewarmRecursive(0)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(calls)/float64(b.N), "loads/op")
}
