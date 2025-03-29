package rldp

import (
	"sync/atomic"
	"time"
)

type TokenBucket struct {
	ratePerSec int64
	capacity   int64

	tokens     int64
	lastRefill int64

	peerName string
}

func NewTokenBucket(rate int64, peerName string) *TokenBucket {
	return &TokenBucket{
		ratePerSec: rate * 1000,
		capacity:   rate * 1000,
		tokens:     rate * 1000,
		lastRefill: time.Now().UnixMicro(),
		peerName:   peerName,
	}
}

func (tb *TokenBucket) SetRate(pps int64) {
	atomic.StoreInt64(&tb.ratePerSec, pps*1000)
	atomic.StoreInt64(&tb.capacity, pps*1000)
	Logger("[RLDP] Peer rate updated:", tb.peerName, pps)
}

func (tb *TokenBucket) GetRate() int64 {
	return atomic.LoadInt64(&tb.ratePerSec) / 1000
}

func (tb *TokenBucket) GetTokensLeft() int64 {
	return atomic.LoadInt64(&tb.tokens) / 1000
}

func (tb *TokenBucket) TryConsume() bool {
	for {
		now := time.Now().UnixMicro()
		last := atomic.LoadInt64(&tb.lastRefill)
		elapsed := now - last

		if elapsed > 0 {
			add := (elapsed * atomic.LoadInt64(&tb.ratePerSec)) / 1_000_000
			newTokens := atomic.LoadInt64(&tb.tokens) + add
			if capacity := atomic.LoadInt64(&tb.capacity); newTokens > capacity {
				newTokens = capacity
			}
			if atomic.CompareAndSwapInt64(&tb.lastRefill, last, now) {
				atomic.StoreInt64(&tb.tokens, newTokens)
			}
		}

		if currTokens := atomic.LoadInt64(&tb.tokens); currTokens >= 1000 { // micro-tokens
			if !atomic.CompareAndSwapInt64(&tb.tokens, currTokens, currTokens-1000) {
				continue
			}
			return true
		}
		return false
	}
}
