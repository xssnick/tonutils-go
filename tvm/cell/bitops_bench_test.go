package cell

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
)

func benchTestAddr(tb testing.TB) *address.Address {
	tb.Helper()
	data := make([]byte, 32)
	for i := range data {
		data[i] = byte(i * 7)
	}
	return address.NewAddress(0, 0, data)
}

// These benchmarks reuse builders/slices across iterations so the measured
// cost is the bit manipulation itself, not the wrapper allocation.

func BenchmarkBitopsStoreSliceReused(b *testing.B) {
	for _, tc := range []struct {
		name string
		data []byte
		bits uint
		pre  uint // bits stored before to force misalignment
	}{
		{"Aligned256", benchPayload256, 256, 0},
		{"Unaligned256", benchPayload256, 256, 3},
		{"Unaligned768", benchPayload768, 768, 5},
	} {
		b.Run(tc.name, func(b *testing.B) {
			bd := BeginCell()
			if tc.pre > 0 {
				bd.MustStoreUInt(0, tc.pre)
			}
			base := bd.BitsUsed()

			b.ReportAllocs()
			b.SetBytes(int64(tc.bits / 8))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := bd.StoreSlice(tc.data, tc.bits); err != nil {
					b.Fatal(err)
				}
				bd.truncateBits(base)
			}
		})
	}
}

func BenchmarkBitopsStoreUIntReused(b *testing.B) {
	bd := BeginCell()
	bd.MustStoreUInt(0, 3) // misalign
	base := bd.BitsUsed()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bd.StoreUInt(0xDEADBEEFCAFE, 48); err != nil {
			b.Fatal(err)
		}
		bd.truncateBits(base)
	}
}

func BenchmarkBitopsStoreAddrReused(b *testing.B) {
	addr := benchTestAddr(b)
	bd := BeginCell()
	base := bd.BitsUsed()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bd.StoreAddr(addr); err != nil {
			b.Fatal(err)
		}
		bd.truncateBits(base)
	}
}

func BenchmarkBitopsPreloadSliceInto(b *testing.B) {
	for _, tc := range []struct {
		name string
		bits uint
		pre  uint
	}{
		{"Aligned256", 256, 0},
		{"Unaligned256", 256, 3},
		{"Unaligned768", 768, 5},
	} {
		b.Run(tc.name, func(b *testing.B) {
			bd := BeginCell()
			if tc.pre > 0 {
				bd.MustStoreUInt(0, tc.pre)
			}
			bd.MustStoreSlice(benchPayload1000, 1000)
			sl := bd.EndCell().MustBeginParse()
			sl.MustLoadUInt(tc.pre)

			var out [128]byte
			b.ReportAllocs()
			b.SetBytes(int64(tc.bits / 8))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := sl.PreloadSliceInto(out[:], tc.bits); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkBitopsPreloadBigUInt(b *testing.B) {
	bd := BeginCell()
	bd.MustStoreUInt(0, 3) // misalign
	bd.MustStoreBigUInt(benchBigUInt256, 256)
	sl := bd.EndCell().MustBeginParse()
	sl.MustLoadUInt(3)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, err := sl.PreloadBigUInt(256)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkBigIntSink = v
	}
}

func BenchmarkBitopsPreloadBigUIntInto(b *testing.B) {
	bd := BeginCell()
	bd.MustStoreUInt(0, 3) // misalign
	bd.MustStoreBigUInt(benchBigUInt256, 256)
	sl := bd.EndCell().MustBeginParse()
	sl.MustLoadUInt(3)
	dst := new(big.Int).Set(benchBigUInt256)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := sl.PreloadBigUIntInto(dst, 256); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkBigIntSink = dst
}

func BenchmarkBitopsLexCompare(b *testing.B) {
	mk := func(tail byte) *Slice {
		bd := BeginCell()
		bd.MustStoreSlice(benchPayload768, 768)
		bd.MustStoreUInt(uint64(tail), 8)
		return bd.EndCell().MustBeginParse()
	}
	a, c := mk(1), mk(2)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if a.LexCompare(c) >= 0 {
			b.Fatal("unexpected compare result")
		}
	}
}

func BenchmarkBitopsCountLeading(b *testing.B) {
	bd := BeginCell()
	if err := bd.StoreSameBit(true, 500); err != nil {
		b.Fatal(err)
	}
	bd.MustStoreUInt(0, 10)
	sl := bd.EndCell().MustBeginParse()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if sl.CountLeading(true) != 500 {
			b.Fatal("unexpected count")
		}
	}
}

func BenchmarkBitopsBitsEqual(b *testing.B) {
	mk := func(payload []byte, offset uint) *Slice {
		bd := BeginCell()
		bd.MustStoreUInt(0, offset)
		bd.MustStoreSlice(payload, 768)
		sl := bd.EndCell().MustBeginParse()
		sl.MustLoadUInt(offset)
		return sl
	}
	a, c := mk(benchPayload768, 3), mk(benchPayload768, 3)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !a.BitsEqual(c) {
			b.Fatal("expected equal")
		}
	}
}

func BenchmarkBitopsBitsEqualMismatch(b *testing.B) {
	mk := func(payload []byte) *Slice {
		bd := BeginCell().MustStoreUInt(0, 3).MustStoreSlice(payload, 768)
		sl := bd.EndCell().MustBeginParse()
		sl.MustLoadUInt(3)
		return sl
	}
	left := mk(benchPayload768)

	for _, tc := range []struct {
		name string
		bit  uint
	}{
		{name: "First", bit: 0},
		{name: "Middle", bit: 384},
		{name: "Last", bit: 767},
	} {
		b.Run(tc.name, func(b *testing.B) {
			payload := bytes.Clone(benchPayload768)
			payload[tc.bit/8] ^= 1 << (7 - tc.bit%8)
			right := mk(payload)

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if left.BitsEqual(right) {
					b.Fatal("expected mismatch")
				}
			}
		})
	}
}

func BenchmarkBitopsBitsEqualDifferentOffsets(b *testing.B) {
	mk := func(offset uint) *Slice {
		bd := BeginCell().MustStoreUInt(0, offset).MustStoreSlice(benchPayload768, 768)
		sl := bd.EndCell().MustBeginParse()
		sl.MustLoadUInt(offset)
		return sl
	}
	left, right := mk(3), mk(5)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !left.BitsEqual(right) {
			b.Fatal("expected equal")
		}
	}
}
