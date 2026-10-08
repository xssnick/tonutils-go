package rldp

import (
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkReviewTokenBucketConsumePacketsAvailable(b *testing.B) {
	for _, tc := range []struct {
		name    string
		packets int
	}{{"one_packet", 1}, {"burst_32", 32}} {
		b.Run(tc.name, func(b *testing.B) {
			const partSize = 1200
			const bursts = 64
			bytesPerBurst := int64(tc.packets * partSize)
			budget := bytesPerBurst * bursts * 1000
			tb := NewTokenBucket(bytesPerBurst*bursts, "benchmark")
			// Isolate consumption with available tokens. A periodic budget
			// reset prevents exhaustion; the refill branch has its own benchmark.
			atomic.StoreInt64(&tb.lastRefill, time.Now().Add(time.Hour).UnixMicro())
			left := bursts
			b.SetBytes(bytesPerBurst)
			b.ReportAllocs()

			for b.Loop() {
				if left == 0 {
					atomic.StoreInt64(&tb.tokens, budget)
					left = bursts
				}
				if got := tb.ConsumePackets(tc.packets, partSize); got != tc.packets {
					b.Fatalf("consumed packets = %d, want %d", got, tc.packets)
				}
				left--
			}
		})
	}
}

func BenchmarkReviewTokenBucketConsumePacketsRefill(b *testing.B) {
	const packets = 32
	const partSize = 1200
	const bytesPerBurst = packets * partSize
	tb := NewTokenBucket(bytesPerBurst*1000, "benchmark")
	b.SetBytes(bytesPerBurst)
	b.ReportAllocs()

	for b.Loop() {
		// Each call refills at least one burst after a modeled 2 ms pacing interval.
		// Fixture stores are identical for both versions and keep every
		// iteration on the refill-and-consume path rather than exhaustion.
		atomic.StoreInt64(&tb.tokens, 0)
		atomic.StoreInt64(&tb.lastRefill, time.Now().UnixMicro()-2000)
		if got := tb.ConsumePackets(packets, partSize); got != packets {
			b.Fatalf("consumed packets after refill = %d, want %d", got, packets)
		}
	}
}

func BenchmarkReviewBBRActiveSendBurst(b *testing.B) {
	tb := NewTokenBucket(1<<20, "benchmark")
	ctrl := NewBBRv2Controller(tb, BBRv2Options{MinRate: 32 << 10})
	b.ReportAllocs()

	for b.Loop() {
		ctrl.OnNewSendBurst()
	}
}
