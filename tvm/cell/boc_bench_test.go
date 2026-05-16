package cell

import (
	"encoding/binary"
	"os"
	"runtime"
	"strconv"
	"testing"
)

const (
	largeBOCBenchDefaultTarget = 300 << 20
	largeBOCBenchLeafBytes     = 127
	largeBOCBenchFanout        = 4

	bocHashIndexSmallCellCount = 500
	bocHashIndexLargeDictKeys  = 1_000_000
)

var (
	benchmarkBOCSink   []byte
	benchmarkCellSink  *Cell
	benchmarkRootsSink []*Cell
)

func BenchmarkBOCReferenceFixture(b *testing.B) {
	root, rawMode31BOC := loadReferenceFixtureRoot(b)
	defaultBOC := root.ToBOC()
	mode2Opts := BOCSerializeOptions{WithCRC32C: true}
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

	b.Run("ParseDefaultNoCopy", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(defaultBOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(defaultBOC), BOCParseOptions{
				NoCopyPayload: true,
			})
			if err != nil {
				b.Fatalf("failed to parse default boc without payload copy: %v", err)
			}
			benchmarkRootsSink = roots
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

	b.Run("ParseMode31NoCopyTrusted", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(rawMode31BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(rawMode31BOC), BOCParseOptions{
				NoCopyPayload: true,
				TrustedHashes: true,
			})
			if err != nil {
				b.Fatalf("failed to parse mode31 boc without payload copy: %v", err)
			}
			benchmarkRootsSink = roots
		}
	})

	b.Run("ParseMode31Lazy", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(rawMode31BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(rawMode31BOC), BOCParseOptions{
				Lazy:          true,
				TrustedHashes: true,
			})
			if err != nil {
				b.Fatalf("failed to parse mode31 boc lazily: %v", err)
			}
			benchmarkRootsSink = roots
		}
	})

	b.Run("ParseMode31LazyMaterialize", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(rawMode31BOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(rawMode31BOC), BOCParseOptions{
				Lazy:          true,
				TrustedHashes: true,
			})
			if err != nil {
				b.Fatalf("failed to parse mode31 boc lazily: %v", err)
			}
			if err = materializeTestCellTree(roots[0]); err != nil {
				b.Fatalf("failed to materialize lazy mode31 boc: %v", err)
			}
			benchmarkRootsSink = roots
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

func BenchmarkBOCLarge300MB(b *testing.B) {
	targetSize := largeBOCBenchmarkTarget(b)
	root, cellCount, estimatedSize := buildLargeBOCBenchmarkRoot(b, targetSize)
	b.Logf("large boc fixture: target=%d estimated=%d cells=%d", targetSize, estimatedSize, cellCount)

	largeBOC := root.ToBOCWithOptions(BOCSerializeOptions{})
	if len(largeBOC) < targetSize {
		b.Fatalf("large boc is smaller than target: got %d, want at least %d", len(largeBOC), targetSize)
	}
	if int64(len(largeBOC)) != estimatedSize {
		b.Fatalf("large boc size mismatch: got %d, estimated %d", len(largeBOC), estimatedSize)
	}
	b.Logf("large boc serialized size: %d bytes", len(largeBOC))

	b.Run("SerializeNoCRC", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(largeBOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			benchmarkBOCSink = root.ToBOCWithOptions(BOCSerializeOptions{})
		}
	})

	benchmarkBOCSink = nil
	root = nil
	runtime.GC()

	b.Run("ParseNoCRC", func(b *testing.B) {
		prevMaxBOCCells := MaxBOCCells
		if MaxBOCCells > 0 && cellCount > MaxBOCCells {
			MaxBOCCells = cellCount
		}
		defer func() {
			MaxBOCCells = prevMaxBOCCells
		}()

		b.ReportAllocs()
		b.SetBytes(int64(len(largeBOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			cell, err := FromBOC(largeBOC)
			if err != nil {
				b.Fatalf("failed to parse large boc: %v", err)
			}
			benchmarkCellSink = cell
		}
	})

	b.Run("ParseNoCRCNoCopy", func(b *testing.B) {
		prevMaxBOCCells := MaxBOCCells
		if MaxBOCCells > 0 && cellCount > MaxBOCCells {
			MaxBOCCells = cellCount
		}
		defer func() {
			MaxBOCCells = prevMaxBOCCells
		}()

		b.ReportAllocs()
		b.SetBytes(int64(len(largeBOC)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(largeBOC), BOCParseOptions{
				NoCopyPayload: true,
			})
			if err != nil {
				b.Fatalf("failed to parse large boc without payload copy: %v", err)
			}
			benchmarkRootsSink = roots
		}
	})
}

func BenchmarkBOCSerializeHashIndex(b *testing.B) {
	b.Run("Tree500Cells", func(b *testing.B) {
		root := buildBOCChainBenchmarkRoot(b, bocHashIndexSmallCellCount)
		benchmarkBOCHashIndexSerialize(b, root, BOCSerializeOptions{})
	})

	b.Run("Dict1MKeys", func(b *testing.B) {
		keys := bocHashIndexBenchmarkLargeKeys(b)
		root := buildBOCDictBenchmarkRoot(b, keys)
		b.Logf("dict keys: %d", keys)
		benchmarkBOCHashIndexSerialize(b, root, BOCSerializeOptions{})
	})
}

func benchmarkBOCHashIndexSerialize(b *testing.B, root *Cell, opts BOCSerializeOptions) {
	b.Helper()

	boc, err := ToBOCWithOptionsErr([]*Cell{root}, opts)
	if err != nil {
		b.Fatalf("failed to serialize boc: %v", err)
	}
	b.Logf("boc size: %d bytes", len(boc))

	b.Run("Serialize", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(boc)))
		b.Cleanup(func() {
			benchmarkBOCSink = nil
			runtime.GC()
		})
		runtime.GC()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			boc, err := ToBOCWithOptionsErr([]*Cell{root}, opts)
			if err != nil {
				b.Fatalf("failed to serialize boc: %v", err)
			}
			benchmarkBOCSink = boc
		}
	})
}

func buildBOCChainBenchmarkRoot(tb testing.TB, cellCount int) *Cell {
	tb.Helper()
	if cellCount <= 0 {
		tb.Fatalf("cell count should be positive, got %d", cellCount)
	}

	const bodyBytes = 16
	data := make([]byte, cellCount*bodyBytes)
	cells := make([]Cell, cellCount)
	for i := range cells {
		body := data[i*bodyBytes : (i+1)*bodyBytes]
		binary.LittleEndian.PutUint64(body, uint64(i))
		binary.LittleEndian.PutUint64(body[8:], bocHashIndexBenchmarkMix(uint64(i)))

		cells[i].data = body
		cells[i].bitsSz = bodyBytes * 8
		if i > 0 {
			cells[i].setRef(0, &cells[i-1])
			cells[i].setRefsCount(1)
			cells[i].setLevelMask(ordinaryLevelMask(cells[i].rawRefs()))
		}
		if err := cells[i].calculateHashes(); err != nil {
			tb.Fatalf("failed to calculate hash for chain cell %d: %v", i, err)
		}
	}

	return &cells[cellCount-1]
}

func buildBOCDictBenchmarkRoot(tb testing.TB, keys int) *Cell {
	tb.Helper()
	if keys <= 0 {
		tb.Fatalf("key count should be positive, got %d", keys)
	}

	dict := NewDict(32)
	for i := 0; i < keys; i++ {
		key := BeginCell().MustStoreUInt(uint64(i), 32).EndCell()
		value := BeginCell().
			MustStoreUInt(uint64(i), 32).
			MustStoreUInt(bocHashIndexBenchmarkMix(uint64(i)), 64).
			EndCell()
		if err := dict.Set(key, value); err != nil {
			tb.Fatalf("failed to set benchmark dict key %d: %v", i, err)
		}
	}

	return dict.AsCell()
}

func bocHashIndexBenchmarkLargeKeys(tb testing.TB) int {
	tb.Helper()

	keys := os.Getenv("TONUTILS_BOC_HASH_INDEX_BENCH_KEYS")
	if keys == "" {
		return bocHashIndexLargeDictKeys
	}

	n, err := strconv.Atoi(keys)
	if err != nil || n <= 0 {
		tb.Fatalf("TONUTILS_BOC_HASH_INDEX_BENCH_KEYS should be a positive integer, got %q", keys)
	}
	return n
}

func bocHashIndexBenchmarkMix(v uint64) uint64 {
	v ^= v >> 30
	v *= 0xbf58476d1ce4e5b9
	v ^= v >> 27
	v *= 0x94d049bb133111eb
	v ^= v >> 31
	return v
}

func largeBOCBenchmarkTarget(tb testing.TB) int {
	tb.Helper()

	mb := os.Getenv("TONUTILS_LARGE_BOC_BENCH_MB")
	if mb == "" {
		return largeBOCBenchDefaultTarget
	}

	n, err := strconv.Atoi(mb)
	if err != nil || n <= 0 {
		tb.Fatalf("TONUTILS_LARGE_BOC_BENCH_MB should be a positive integer, got %q", mb)
	}
	return n << 20
}

func buildLargeBOCBenchmarkRoot(tb testing.TB, targetSize int) (*Cell, int, int64) {
	tb.Helper()

	leafCount, estimatedSize, cellCount := largeBOCBenchmarkLeafCount(targetSize)
	leafData := make([]byte, leafCount*largeBOCBenchLeafBytes)
	level := make([]Cell, leafCount)

	for i := range level {
		off := i * largeBOCBenchLeafBytes
		data := leafData[off : off+largeBOCBenchLeafBytes]
		binary.LittleEndian.PutUint64(data, uint64(i))
		binary.LittleEndian.PutUint64(data[8:], uint64(i)*0x9e3779b97f4a7c15)
		binary.LittleEndian.PutUint64(data[16:], uint64(leafCount-i))

		level[i].data = data
		level[i].bitsSz = largeBOCBenchLeafBytes * 8
		if err := level[i].calculateHashes(); err != nil {
			tb.Fatalf("failed to calculate leaf hash %d: %v", i, err)
		}
	}

	for len(level) > 1 {
		parentCount := (len(level) + largeBOCBenchFanout - 1) / largeBOCBenchFanout
		parents := make([]Cell, parentCount)
		for i := range parents {
			start := i * largeBOCBenchFanout
			end := min(start+largeBOCBenchFanout, len(level))
			for refIdx, childIdx := 0, start; childIdx < end; refIdx, childIdx = refIdx+1, childIdx+1 {
				parents[i].setRef(refIdx, &level[childIdx])
			}
			parents[i].setRefsCount(end - start)
			parents[i].setLevelMask(ordinaryLevelMask(parents[i].rawRefs()))
			if err := parents[i].calculateHashes(); err != nil {
				tb.Fatalf("failed to calculate parent hash at level size %d index %d: %v", len(level), i, err)
			}
		}
		level = parents
	}

	return &level[0], cellCount, estimatedSize
}

func largeBOCBenchmarkLeafCount(targetSize int) (int, int64, int) {
	hi := 1
	for {
		_, totalSize, _ := estimateLargeBOCBenchmarkSize(hi)
		if totalSize >= int64(targetSize) {
			break
		}
		hi *= 2
	}

	lo := 1
	for lo < hi {
		mid := lo + (hi-lo)/2
		_, totalSize, _ := estimateLargeBOCBenchmarkSize(mid)
		if totalSize >= int64(targetSize) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}

	_, totalSize, cellCount := estimateLargeBOCBenchmarkSize(lo)
	return lo, totalSize, cellCount
}

func estimateLargeBOCBenchmarkSize(leafCount int) (int64, int64, int) {
	cellCount := leafCount
	dataSize := int64(leafCount * (2 + largeBOCBenchLeafBytes))

	for levelSize := leafCount; levelSize > 1; {
		parentCount := (levelSize + largeBOCBenchFanout - 1) / largeBOCBenchFanout
		cellCount += parentCount
		levelSize = parentCount
	}

	refByteSize := dynamicBOCBenchmarkIntSize(uint64(cellCount))

	for levelSize := leafCount; levelSize > 1; {
		parentCount := (levelSize + largeBOCBenchFanout - 1) / largeBOCBenchFanout
		dataSize += int64(parentCount * 2)
		dataSize += int64(levelSize * refByteSize)
		levelSize = parentCount
	}

	offsetByteSize := dynamicBOCBenchmarkIntSize(uint64(dataSize))
	totalSize := dataSize + int64(6+4*refByteSize+offsetByteSize)
	return dataSize, totalSize, cellCount
}

func dynamicBOCBenchmarkIntSize(v uint64) int {
	size := 0
	for v >= (uint64(1) << uint(size*8)) {
		size++
	}
	return size
}
