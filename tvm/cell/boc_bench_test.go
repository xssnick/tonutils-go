package cell

import "testing"

var (
	benchmarkBOCSink   []byte
	benchmarkCellSink  *Cell
	benchmarkRootsSink []*Cell
)

func BenchmarkBOCReferenceFixture(b *testing.B) {
	root, rawMode31BOC := loadReferenceFixtureRoot(b)
	defaultBOC := root.ToBOC()
	mode2Opts := BOCOptions{WithCRC32C: true}
	mode2BOC := root.ToBOCWithOptions(mode2Opts)
	mode31Opts := mode31Options()

	baselineCompressed, err := CompressBOC([]*Cell{root}, CompressionBaselineLZ4, nil)
	if err != nil {
		b.Fatalf("failed to prepare baseline compressed boc: %v", err)
	}

	improvedCompressed, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4, nil)
	if err != nil {
		b.Fatalf("failed to prepare improved compressed boc: %v", err)
	}

	b.Run("ParseMode2", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(mode2BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			cell, err := FromBOC(mode2BOC)
			if err != nil {
				b.Fatalf("failed to parse mode2 boc: %v", err)
			}
			benchmarkCellSink = cell
		}
	})

	b.Run("ParseDefault", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(defaultBOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			cell, err := FromBOC(defaultBOC)
			if err != nil {
				b.Fatalf("failed to parse default boc: %v", err)
			}
			benchmarkCellSink = cell
		}
	})

	b.Run("ParseMode31", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(rawMode31BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			cell, err := FromBOC(rawMode31BOC)
			if err != nil {
				b.Fatalf("failed to parse mode31 boc: %v", err)
			}
			benchmarkCellSink = cell
		}
	})

	b.Run("SerializeDefault", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(defaultBOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			benchmarkBOCSink = root.ToBOC()
		}
	})

	b.Run("SerializeMode2", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(mode2BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			benchmarkBOCSink = root.ToBOCWithOptions(mode2Opts)
		}
	})

	b.Run("SerializeMode31", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(rawMode31BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			benchmarkBOCSink = root.ToBOCWithOptions(mode31Opts)
		}
	})

	b.Run("ComputeFileHash", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(rawMode31BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			benchmarkBOCSink = ComputeFileHash(root)
		}
	})

	b.Run("CompressBaselineLZ4", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(mode2BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			data, err := CompressBOC([]*Cell{root}, CompressionBaselineLZ4, nil)
			if err != nil {
				b.Fatalf("failed to compress baseline boc: %v", err)
			}
			benchmarkBOCSink = data
		}
	})

	b.Run("DecompressBaselineLZ4", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(mode2BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			roots, err := DecompressBOC(baselineCompressed, len(mode2BOC)+1024, nil)
			if err != nil {
				b.Fatalf("failed to decompress baseline boc: %v", err)
			}
			benchmarkRootsSink = roots
		}
	})

	b.Run("CompressImprovedStructureLZ4", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(mode2BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			data, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4, nil)
			if err != nil {
				b.Fatalf("failed to compress improved-structure boc: %v", err)
			}
			benchmarkBOCSink = data
		}
	})

	b.Run("DecompressImprovedStructureLZ4", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(mode2BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			roots, err := DecompressBOC(improvedCompressed, len(mode2BOC)+1024, nil)
			if err != nil {
				b.Fatalf("failed to decompress improved-structure boc: %v", err)
			}
			benchmarkRootsSink = roots
		}
	})
}

func BenchmarkBOCStateAwareCompressionFixture(b *testing.B) {
	root, state, mode31BOC := buildStateAwareCompressionFixture(b)
	compressed, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4WithState, state)
	if err != nil {
		b.Fatalf("failed to prepare state-aware compressed boc: %v", err)
	}

	b.Run("CompressImprovedStructureLZ4WithState", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(mode31BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			data, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4WithState, state)
			if err != nil {
				b.Fatalf("failed to compress state-aware boc: %v", err)
			}
			benchmarkBOCSink = data
		}
	})

	b.Run("DecompressImprovedStructureLZ4WithState", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(mode31BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			roots, err := DecompressBOC(compressed, len(mode31BOC)+1024, state)
			if err != nil {
				b.Fatalf("failed to decompress state-aware boc: %v", err)
			}
			benchmarkRootsSink = roots
		}
	})
}
