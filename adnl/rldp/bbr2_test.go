package rldp

import (
	"sync"
	"testing"
	"time"
)

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", msg)
}

func newBBR(t *testing.T, initRate int64, opts BBRv2Options) (*BBRv2Controller, *TokenBucket) {
	t.Helper()
	tb := NewTokenBucket(initRate, "test-peer")
	return NewBBRv2Controller(tb, opts), tb
}

func TestBBR_StartupIncreasesRate(t *testing.T) {
	opts := BBRv2Options{
		MinRate:            20_000,
		MaxRate:            0,
		DefaultRTTMs:       20,
		MinSampleMs:        10,
		BtlBwWindowSec:     2,
		ProbeBwCycleMs:     50,
		ProbeRTTDurationMs: 40,
		MinRTTExpiryMs:     5_000,
		HighLoss:           0.05,
		Beta:               0.85,
	}
	bbr, tb := newBBR(t, opts.MinRate, opts)

	for i := 0; i < 60; i++ {
		bbr.ObserveDelta(100_000, 100_000)
		time.Sleep(12 * time.Millisecond)
	}

	waitUntil(t, 2*time.Second, func() bool {
		return bbr.pacingRate.Load() > opts.MinRate
	}, "pacingRate should increase in Startup")

	if tb.GetRate() != bbr.pacingRate.Load() {
		t.Fatalf("limiter rate mismatch: tb=%d bbr=%d", tb.GetRate(), bbr.pacingRate.Load())
	}
}

func TestBBR_HighLossReducesRate(t *testing.T) {
	opts := BBRv2Options{
		MinRate:            50_000,
		DefaultRTTMs:       25,
		MinSampleMs:        10,
		BtlBwWindowSec:     2,
		ProbeBwCycleMs:     50,
		ProbeRTTDurationMs: 50,
		MinRTTExpiryMs:     5_000,
		HighLoss:           0.05,
		Beta:               0.85,
	}
	bbr, _ := newBBR(t, opts.MinRate, opts)

	for i := 0; i < 12; i++ {
		bbr.ObserveDelta(200_000, 200_000)
		time.Sleep(12 * time.Millisecond)
	}
	r1 := bbr.pacingRate.Load()

	for i := 0; i < 8; i++ {
		bbr.ObserveDelta(100_000, 80_000)
		time.Sleep(12 * time.Millisecond)
	}
	r2 := bbr.pacingRate.Load()
	if r2 >= r1 {
		t.Fatalf("expected rate drop on high loss: before=%d after=%d", r1, r2)
	}
}

func TestBBR_ProbeRTT_EnterAndExit(t *testing.T) {
	opts := BBRv2Options{
		MinRate:            30_000,
		DefaultRTTMs:       10,
		MinSampleMs:        10,
		BtlBwWindowSec:     2,
		ProbeBwCycleMs:     40,
		ProbeRTTDurationMs: 30,
		MinRTTExpiryMs:     60,
		HighLoss:           0.1,
		Beta:               0.9,
	}
	bbr, _ := newBBR(t, opts.MinRate, opts)

	// немного трафика
	for i := 0; i < 5; i++ {
		bbr.ObserveDelta(50_000, 50_000)
		time.Sleep(12 * time.Millisecond)
	}

	time.Sleep(70 * time.Millisecond)
	for i := 0; i < 10 && bbr.state.Load() != 3; i++ {
		bbr.ObserveDelta(1_000, 1_000)
		time.Sleep(12 * time.Millisecond)
	}
	if bbr.state.Load() != 3 {
		t.Fatalf("should enter ProbeRTT")
	}

	for i := 0; i < 20 && bbr.state.Load() != 2; i++ {
		bbr.ObserveDelta(1_000, 1_000)
		time.Sleep(12 * time.Millisecond)
	}
	if bbr.state.Load() != 2 {
		t.Fatalf("should exit to ProbeBW")
	}
}

func TestBBR_RespectsMinMaxRate(t *testing.T) {
	opts := BBRv2Options{
		MinRate:            10_000,
		MaxRate:            15_000,
		DefaultRTTMs:       10,
		MinSampleMs:        10,
		ProbeBwCycleMs:     40,
		ProbeRTTDurationMs: 40,
		MinRTTExpiryMs:     5_000,
		HighLoss:           0.05,
		Beta:               0.85,
	}
	bbr, _ := newBBR(t, opts.MinRate, opts)

	for i := 0; i < 25; i++ {
		bbr.ObserveDelta(1_000_000, 1_000_000)
		time.Sleep(12 * time.Millisecond)
	}
	if got := bbr.pacingRate.Load(); got > opts.MaxRate {
		t.Fatalf("rate exceeded MaxRate: got=%d max=%d", got, opts.MaxRate)
	}

	for i := 0; i < 5; i++ {
		bbr.ObserveDelta(100_000, 20_000) // 80% loss
		time.Sleep(12 * time.Millisecond)
	}
	if got := bbr.pacingRate.Load(); got < opts.MinRate {
		t.Fatalf("rate fell below MinRate: got=%d min=%d", got, opts.MinRate)
	}
}

func TestBBR_SmoothingBounds(t *testing.T) {
	opts := BBRv2Options{
		MinRate:            80_000,
		DefaultRTTMs:       20,
		MinSampleMs:        10,
		BtlBwWindowSec:     2,
		ProbeBwCycleMs:     50,
		ProbeRTTDurationMs: 50,
		MinRTTExpiryMs:     5_000,
		HighLoss:           0.05,
		Beta:               0.85,
	}
	bbr, _ := newBBR(t, opts.MinRate, opts)

	for i := 0; i < 10; i++ {
		bbr.ObserveDelta(500_000, 500_000)
		time.Sleep(12 * time.Millisecond)
	}
	prev := bbr.pacingRate.Load()

	bbr.ObserveDelta(10_000_000, 10_000_000)
	time.Sleep(14 * time.Millisecond)
	now := bbr.pacingRate.Load()
	if now > int64(float64(prev)*1.55) {
		t.Fatalf("up-smoothing failed: prev=%d now=%d", prev, now)
	}

	bbr.ObserveDelta(1_000_000, 100_000) // 90% loss
	time.Sleep(14 * time.Millisecond)
	after := bbr.pacingRate.Load()
	if after < int64(float64(now)*0.65) {
		t.Fatalf("down-smoothing failed: now=%d after=%d", now, after)
	}
}

func TestBBR_BtlBwDecay(t *testing.T) {
	opts := BBRv2Options{
		MinRate:            10_000,
		DefaultRTTMs:       20,
		MinSampleMs:        10,
		BtlBwWindowSec:     1,
		ProbeBwCycleMs:     100,
		ProbeRTTDurationMs: 100,
		MinRTTExpiryMs:     10_000,
		HighLoss:           0.2,
		Beta:               0.85,
	}
	bbr, _ := newBBR(t, opts.MinRate, opts)

	for i := 0; i < 6; i++ {
		bbr.ObserveDelta(200_000, 200_000)
		time.Sleep(12 * time.Millisecond)
	}
	peak := bbr.btlbw.Load()
	if peak <= opts.MinRate {
		t.Fatalf("unexpected peak btlbw: %d", peak)
	}

	winMs := int64(opts.BtlBwWindowSec * 1000)
	bbr.cycleStamp.Store(time.Now().UnixMilli() - winMs - 10)
	bbr.ObserveDelta(1, 1)
	time.Sleep(12 * time.Millisecond)

	decayed := bbr.btlbw.Load()
	if !(decayed < peak && decayed >= opts.MinRate) {
		t.Fatalf("expected btlbw decay: before=%d after=%d (min=%d)", peak, decayed, opts.MinRate)
	}
}

func approxI64(a, b, tol int64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestSendClock_OnSendAndSentAt(t *testing.T) {
	sc := NewSendClock(1024) // power of two
	seq := uint32(42)

	now := time.Now().UnixMilli()
	sc.OnSend(seq, now)

	got, ok := sc.SentAt(seq)
	if !ok {
		t.Fatalf("SentAt(%d) ok=false, want true", seq)
	}
	// допускаем небольшую разницу (квант времени)
	if !approxI64(got, now, 3) {
		t.Fatalf("SentAt ms mismatch: got=%d want~=%d", got, now)
	}
}

func TestSendClock_CollisionOverwritesOld(t *testing.T) {
	sc := NewSendClock(8) // mask=7
	base := time.Now().UnixMilli()

	seq1 := uint32(10) // 10 & 7 = 2
	seq2 := uint32(18) // 18 & 7 = 2

	sc.OnSend(seq1, base+1)
	sc.OnSend(seq2, base+2)

	if _, ok := sc.SentAt(seq1); ok {
		t.Fatalf("expected collision overwrite for seq1=%d", seq1)
	}
	got2, ok2 := sc.SentAt(seq2)
	if !ok2 || got2 != base+2 {
		t.Fatalf("seq2 not found or bad ts: ok=%v got=%d want=%d", ok2, got2, base+2)
	}
}

func TestSendClock_RecentWindowVisibleAfterWrap(t *testing.T) {
	capPow2 := 16
	sc := NewSendClock(capPow2)
	start := time.Now().UnixMilli()

	for i := 0; i < 1000; i++ {
		sc.OnSend(uint32(i), start+int64(i))
	}
	for i := 1000 - capPow2; i < 1000; i++ {
		if _, ok := sc.SentAt(uint32(i)); !ok {
			t.Fatalf("recent seq=%d not found", i)
		}
	}
}

func TestSendClock_ConcurrentRaces(t *testing.T) {
	sc := NewSendClock(4096)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		base := time.Now().UnixMilli()
		for i := uint32(1); i < 50000; i++ {
			sc.OnSend(i, base+int64(i%5000))
		}
	}()

	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := uint32(1); i < 50000; i++ {
				sc.SentAt(i)
			}
		}()
	}

	wg.Wait()
}
