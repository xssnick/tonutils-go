package rldp

import (
	"math"
	"sync/atomic"
	"time"
)

// TokenBucket bytes/sec
type TokenBucket struct {
	ratePerSec int64
	capacity   int64
	tokens     int64

	lastRefill int64 // UnixMicro

	peerName string
}

// NewTokenBucket create bucket with bytes/sec.
func NewTokenBucket(bps int64, peerName string) *TokenBucket {
	if bps < 1 {
		bps = 1
	}
	x := bps * 1000 // burst 1 sec

	return &TokenBucket{
		ratePerSec: x,
		capacity:   x,
		tokens:     x,
		lastRefill: time.Now().UnixMicro(),
		peerName:   peerName,
	}
}

func (tb *TokenBucket) SetCapacityBytes(burstBytes int64) {
	if burstBytes < 0 {
		burstBytes = 0
	}
	capMicro := toMicroBytes(burstBytes)
	atomic.StoreInt64(&tb.capacity, capMicro)

	for {
		curr := atomic.LoadInt64(&tb.tokens)
		if curr <= capMicro {
			break
		}
		if atomic.CompareAndSwapInt64(&tb.tokens, curr, capMicro) {
			break
		}
	}
}

func (tb *TokenBucket) SetRate(bps int64) {
	if bps < 8<<10 { // 8KB/s
		bps = 8 << 10
	} else if bps > 500<<20 { // 500 MB/s
		bps = 500 << 20
	}
	atomic.StoreInt64(&tb.ratePerSec, bps*1000)

	curCap := atomic.LoadInt64(&tb.capacity)
	curRate := atomic.LoadInt64(&tb.ratePerSec)

	// if cap ~= old rate, use new
	if abs64(curCap-curRate) < curRate/64 { // ~1.5%
		atomic.StoreInt64(&tb.capacity, curRate)
	}

	Logger("[RLDP] Peer pacing updated (Bps):", tb.peerName, bps)
}

func (tb *TokenBucket) GetRate() int64 {
	return atomic.LoadInt64(&tb.ratePerSec) / 1000
}

func (tb *TokenBucket) GetTokensLeft() int64 {
	return atomic.LoadInt64(&tb.tokens) / 1000
}

func (tb *TokenBucket) ConsumeUpTo(maxBytes int) int {
	if maxBytes <= 0 {
		return 0
	}
	req := int64(maxBytes)

	for {
		now := time.Now().UnixMicro()
		last := atomic.LoadInt64(&tb.lastRefill)
		elapsed := now - last

		if elapsed > 0 {
			add := (elapsed * atomic.LoadInt64(&tb.ratePerSec)) / 1_000_000
			if add > 0 && atomic.CompareAndSwapInt64(&tb.lastRefill, last, now) {
				for {
					curr := atomic.LoadInt64(&tb.tokens)
					newTokens := curr + add
					capacity := atomic.LoadInt64(&tb.capacity)
					if newTokens > capacity {
						newTokens = capacity
					}

					if atomic.CompareAndSwapInt64(&tb.tokens, curr, newTokens) {
						break
					}
				}
			}
		}

		currTokens := atomic.LoadInt64(&tb.tokens)
		availableBytes := currTokens / 1000
		if availableBytes <= 0 {
			return 0
		}

		toConsume := req
		if availableBytes < toConsume {
			toConsume = availableBytes
		}

		micro := toConsume * 1000
		if atomic.CompareAndSwapInt64(&tb.tokens, currTokens, currTokens-micro) {
			return int(toConsume)
		}

		// race, repeat
	}
}

func (tb *TokenBucket) ConsumePackets(maxPackets, partSize int) int {
	if maxPackets <= 0 || partSize <= 0 {
		return 0
	}
	wantBytes := int64(maxPackets) * int64(partSize)
	gotBytes := tb.ConsumeUpTo(int(wantBytes))
	return gotBytes / partSize
}

// SetBurst implements the interface used by the BBR controller for seeding
// the token bucket with a specific burst size (in bytes).
func (tb *TokenBucket) SetBurst(burstBytes int64) {
	tb.SetCapacityBytes(burstBytes)
}

// AddTokens adds up to n bytes worth of tokens into the bucket.
// The total amount of tokens is always capped at the configured capacity.
func (tb *TokenBucket) AddTokens(n int64) {
	if n <= 0 {
		return
	}

	add := toMicroBytes(n)
	for {
		curr := atomic.LoadInt64(&tb.tokens)
		cp := atomic.LoadInt64(&tb.capacity)
		newVal := curr + add
		if cp > 0 && newVal > cp {
			newVal = cp
		}
		if atomic.CompareAndSwapInt64(&tb.tokens, curr, newVal) {
			return
		}
	}
}

func (tb *TokenBucket) TryConsumeBytes(n int) bool {
	return tb.ConsumeUpTo(n) == n
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func toMicroBytes(v int64) int64 {
	if v >= math.MaxInt64/1000 {
		return math.MaxInt64
	}
	if v <= math.MinInt64/1000 {
		return math.MinInt64
	}
	return v * 1000
}
