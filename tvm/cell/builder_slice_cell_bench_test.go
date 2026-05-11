package cell

import (
	"bytes"
	"math/big"
	"testing"
)

var (
	benchmarkBuilderSink *Builder
	benchmarkSliceSink   *Slice
	benchmarkUint64Sink  uint64
	benchmarkBigIntSink  *big.Int
	benchmarkBoolSink    bool

	benchPayload256  = bytes.Repeat([]byte{0xAB}, 32)
	benchPayload257  = append(bytes.Repeat([]byte{0xCD}, 32), 0x80)
	benchPayload768  = bytes.Repeat([]byte{0xEF}, 96)
	benchPayload1000 = bytes.Repeat([]byte{0xA5}, 125)

	benchBigUInt255 = new(big.Int).SetBytes(append([]byte{0x7F}, bytes.Repeat([]byte{0xAA}, 31)...))
	benchBigUInt256 = new(big.Int).SetBytes(bytes.Repeat([]byte{0xBB}, 32))
)

func benchSmallCell() *Cell {
	return BeginCell().
		MustStoreUInt(0xAABBCCDD, 32).
		MustStoreUInt(0x11223344, 32).
		EndCell()
}

func benchLargeBuilder() *Builder {
	left := BeginCell().MustStoreSlice(benchPayload256, 256).EndCell()
	right := BeginCell().MustStoreSlice(benchPayload257, 257).EndCell()

	return BeginCell().
		MustStoreSlice(benchPayload1000, 1000).
		MustStoreRef(left).
		MustStoreRef(right)
}

func benchLargeCell() *Cell {
	return benchLargeBuilder().EndCell()
}

func BenchmarkBuilderStoreUInt(b *testing.B) {
	benchmarks := []struct {
		name  string
		value uint64
		bits  uint
	}{
		{name: "1bit", value: 1, bits: 1},
		{name: "32bit", value: 0xAABBCCDD, bits: 32},
		{name: "64bit", value: 0x1122334455667788, bits: 64},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				builder := BeginCell()
				if err := builder.StoreUInt(bm.value, bm.bits); err != nil {
					b.Fatal(err)
				}
				benchmarkBuilderSink = builder
			}
		})
	}
}

func BenchmarkBuilderStoreBoolBit(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		builder := BeginCell()
		if err := builder.StoreBoolBit(true); err != nil {
			b.Fatal(err)
		}
		benchmarkBuilderSink = builder
	}
}

func BenchmarkBuilderStoreSlice(b *testing.B) {
	benchmarks := []struct {
		name    string
		payload []byte
		bits    uint
	}{
		{name: "Aligned256", payload: benchPayload256, bits: 256},
		{name: "Unaligned257", payload: benchPayload257, bits: 257},
		{name: "Large768", payload: benchPayload768, bits: 768},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64((bm.bits + 7) / 8))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				builder := BeginCell()
				if err := builder.StoreSlice(bm.payload, bm.bits); err != nil {
					b.Fatal(err)
				}
				benchmarkBuilderSink = builder
			}
		})
	}
}

func BenchmarkBuilderStoreBigUInt(b *testing.B) {
	benchmarks := []struct {
		name  string
		value *big.Int
		bits  uint
	}{
		{name: "Unaligned255", value: benchBigUInt255, bits: 255},
		{name: "Aligned256", value: benchBigUInt256, bits: 256},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				builder := BeginCell()
				if err := builder.StoreBigUInt(bm.value, bm.bits); err != nil {
					b.Fatal(err)
				}
				benchmarkBuilderSink = builder
			}
		})
	}
}

func BenchmarkBuilderEndCell(b *testing.B) {
	builder := benchLargeBuilder()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkCellSink = builder.EndCell()
	}
}

func BenchmarkBuilderToSlice(b *testing.B) {
	builder := benchLargeBuilder()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkSliceSink = builder.ToSlice()
	}
}

func BenchmarkSliceLoadUInt(b *testing.B) {
	benchmarks := []struct {
		name string
		cell *Cell
		bits uint
	}{
		{name: "1bit", cell: BeginCell().MustStoreUInt(1, 1).EndCell(), bits: 1},
		{name: "32bit", cell: BeginCell().MustStoreUInt(0xAABBCCDD, 32).EndCell(), bits: 32},
		{name: "64bit", cell: BeginCell().MustStoreUInt(0x1122334455667788, 64).EndCell(), bits: 64},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				slice := bm.cell.BeginParse()
				v, err := slice.LoadUInt(bm.bits)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkUint64Sink = v
			}
		})
	}
}

func BenchmarkSliceLoadBoolBit(b *testing.B) {
	cell := BeginCell().MustStoreBoolBit(true).EndCell()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		slice := cell.BeginParse()
		v, err := slice.LoadBoolBit()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkBoolSink = v
	}
}

func BenchmarkSliceLoadSlice(b *testing.B) {
	benchmarks := []struct {
		name string
		cell *Cell
		bits uint
	}{
		{name: "Aligned256", cell: BeginCell().MustStoreSlice(benchPayload256, 256).EndCell(), bits: 256},
		{name: "Unaligned257", cell: BeginCell().MustStoreSlice(benchPayload257, 257).EndCell(), bits: 257},
		{name: "Large768", cell: BeginCell().MustStoreSlice(benchPayload768, 768).EndCell(), bits: 768},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64((bm.bits + 7) / 8))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				slice := bm.cell.BeginParse()
				out, err := slice.LoadSlice(bm.bits)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkBOCSink = out
			}
		})
	}
}

func BenchmarkSliceLoadBigUInt(b *testing.B) {
	benchmarks := []struct {
		name string
		cell *Cell
		bits uint
	}{
		{name: "Unaligned255", cell: BeginCell().MustStoreBigUInt(benchBigUInt255, 255).EndCell(), bits: 255},
		{name: "Aligned256", cell: BeginCell().MustStoreBigUInt(benchBigUInt256, 256).EndCell(), bits: 256},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				slice := bm.cell.BeginParse()
				v, err := slice.LoadBigUInt(bm.bits)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkBigIntSink = v
			}
		})
	}
}

func BenchmarkSliceToBuilder(b *testing.B) {
	slice := benchLargeCell().BeginParse()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkBuilderSink = slice.ToBuilder()
	}
}

func BenchmarkSliceToCell(b *testing.B) {
	slice := benchLargeCell().BeginParse()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cell, err := slice.ToCell()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkCellSink = cell
	}
}

func BenchmarkCellBeginParse(b *testing.B) {
	benchmarks := []struct {
		name string
		cell *Cell
	}{
		{name: "Small", cell: benchSmallCell()},
		{name: "Large", cell: benchLargeCell()},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchmarkSliceSink = bm.cell.BeginParse()
			}
		})
	}
}

func BenchmarkCellToBuilder(b *testing.B) {
	benchmarks := []struct {
		name string
		cell *Cell
	}{
		{name: "Small", cell: benchSmallCell()},
		{name: "Large", cell: benchLargeCell()},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchmarkBuilderSink = bm.cell.ToBuilder()
			}
		})
	}
}
