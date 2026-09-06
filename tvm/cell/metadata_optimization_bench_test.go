package cell

import (
	"fmt"
	"testing"
)

var metadataBenchmarkSink Metadata

func BenchmarkGetMetadataPacked(b *testing.B) {
	leaf := BeginCell().MustStoreUInt(42, 64).EndCell()
	for _, refs := range []int{0, 1, 2, 4} {
		builder := BeginCell().MustStoreUInt(17, 8)
		for range refs {
			builder.MustStoreRef(leaf)
		}
		root := builder.EndCell()
		b.Run(fmt.Sprintf("refs%d", refs), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				metadataBenchmarkSink = root.GetMetadata()
			}
		})
	}
}
