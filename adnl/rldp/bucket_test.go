package rldp

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func wait(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

func TestTokenBucket_Init(t *testing.T) {
	tb := NewTokenBucket(100_000, "peer") // 100 KB/s
	if got := tb.GetRate(); got != 100_000 {
		t.Fatalf("GetRate=%d want=100000", got)
	}

	if got := tb.GetTokensLeft(); got != 100_000 {
		t.Fatalf("GetTokensLeft init=%d want=100000", got)
	}
}

func TestTokenBucket_Consume_And_Exhaust(t *testing.T) {
	tb := NewTokenBucket(10_000, "peer") // 10 KB/s
	if n := tb.ConsumeUpTo(4_000); n != 4_000 {
		t.Fatalf("ConsumeUpTo 4k -> %d want 4000", n)
	}
	if left := tb.GetTokensLeft(); left != 6_000 {
		t.Fatalf("left=%d want 6000", left)
	}

	if n := tb.ConsumeUpTo(7_000); n != 6_000 {
		t.Fatalf("ConsumeUpTo 7k -> %d want 6000 (cap by available)", n)
	}

	if left := tb.GetTokensLeft(); left != 0 {
		t.Fatalf("left after exhaust=%d want 0", left)
	}

	if n := tb.ConsumeUpTo(1_000); n != 0 {
		t.Fatalf("ConsumeUpTo when empty -> %d want 0", n)
	}
}

func TestTokenBucket_RefillOverTime(t *testing.T) {
	tb := NewTokenBucket(10_000, "peer") // 10 KB/s
	_ = tb.ConsumeUpTo(10_000)
	if left := tb.GetTokensLeft(); left != 0 {
		t.Fatalf("left=%d want 0", left)
	}

	wait(120)

	n := tb.ConsumeUpTo(10_000)
	if n < 800 || n > 2_000 {
		t.Fatalf("refill bytes=%d want around 1200 (800..2000)", n)
	}
}

func TestTokenBucket_SetRate_DownAndUp(t *testing.T) {
	tb := NewTokenBucket(40_000, "peer") // 40 KB/s
	_ = tb.ConsumeUpTo(40_000)
	tb.SetRate(10_000)
	wait(110)
	n := tb.ConsumeUpTo(10_000)
	if n < 700 || n > 2_000 {
		t.Fatalf("after downrate refill=%d want ~1100 (700..2000)", n)
	}

	tb.SetRate(200_000)
	_ = tb.ConsumeUpTo(200_000)
	wait(100) // ~20_000 B
	n2 := tb.ConsumeUpTo(1_000_000)
	if n2 < 12_000 || n2 > 40_000 {
		t.Fatalf("after uprate refill=%d want ~20000 (12000..40000)", n2)
	}
}

func TestTokenBucket_SetCapacityBytes_Burst(t *testing.T) {
	tb := NewTokenBucket(50_000, "peer")
	tb.SetCapacityBytes(10_000)

	_ = tb.ConsumeUpTo(1_000_000)
	wait(250)
	got := tb.ConsumeUpTo(1_000_000)
	if got < 9_000 || got > 10_000 {
		t.Fatalf("burst-capped consume=%d want ~10k (9000..10000)", got)
	}
}

func TestTokenBucket_ConsumePackets(t *testing.T) {
	tb := NewTokenBucket(12_000, "peer") // 12 kB/s
	gotPk := tb.ConsumePackets(100, 1_200)
	if gotPk != 10 { // 12k / 1.2k = 10
		t.Fatalf("ConsumePackets first=%d want 10", gotPk)
	}

	wait(105)

	gotPk2 := tb.ConsumePackets(100, 1_200)
	if gotPk2 != 1 {
		t.Fatalf("ConsumePackets after refill=%d want 1", gotPk2)
	}
}

func TestTokenBucket_TryConsumeBytes(t *testing.T) {
	tb := NewTokenBucket(5_000, "peer")
	if ok := tb.TryConsumeBytes(512); !ok {
		t.Fatalf("TryConsumeBytes(512) = false, want true")
	}

	left := tb.GetTokensLeft()
	if left < 4_400 || left > 4_500 {
		t.Fatalf("left ~ 4488.., got %d", left)
	}

	if ok := tb.TryConsumeBytes(10_000); ok {
		t.Fatalf("TryConsumeBytes big should be false")
	}
}

func TestTokenBucket_ParallelConsume_NoOveruse(t *testing.T) {
	tb := NewTokenBucket(100_000, "peer") // 100k B/s
	testDur := 100 * time.Millisecond
	start := time.Now()
	var consumed atomic.Int64

	wg := sync.WaitGroup{}
	workers := 8
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for time.Since(start) < testDur {
				n := tb.ConsumeUpTo(1_500)
				if n > 0 {
					consumed.Add(int64(n))
				} else {
					time.Sleep(200 * time.Microsecond)
				}
			}
		}()
	}
	wg.Wait()

	got := consumed.Load()
	if got > 120_000 {
		t.Fatalf("parallel consumed=%d exceeds expected ~<=120000", got)
	}
}
