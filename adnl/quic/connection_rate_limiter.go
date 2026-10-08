package quic

import (
	"net"
	"sync"
	"time"
)

const connectionRateLimiterCleanupPeriod = 10 * time.Second

type rateLimitState struct {
	readyAt time.Time
}

func (s *rateLimitState) take(now time.Time, capacity int, period time.Duration) bool {
	emissionInterval := time.Duration(capacity-1) * period
	minReadyAt := now.Add(-emissionInterval)
	if s.readyAt.IsZero() || s.readyAt.Before(minReadyAt) {
		s.readyAt = minReadyAt
	}
	if s.readyAt.After(now) {
		return false
	}
	s.readyAt = s.readyAt.Add(period)
	return true
}

func (s *rateLimitState) full(now time.Time, capacity int, period time.Duration) bool {
	emissionInterval := time.Duration(capacity-1) * period
	return s.readyAt.Before(now.Add(-emissionInterval))
}

// newConnectionRateLimiter mirrors the C++ QUIC GCRA policy: a small burst
// and sustained rate per source IP, followed by a high-capacity global guard.
// Entries that have fully refilled are discarded periodically.
type newConnectionRateLimiter struct {
	mu sync.Mutex

	perIPCapacity  int
	perIPPeriod    time.Duration
	globalCapacity int
	globalPeriod   time.Duration
	maxEntries     int

	perIP     map[remoteAddressKey]rateLimitState
	global    rateLimitState
	cleanupAt time.Time
}

func newConnectionRateLimits(limits Limits) *newConnectionRateLimiter {
	return &newConnectionRateLimiter{
		perIPCapacity:  limits.NewConnectionRateLimitCapacity,
		perIPPeriod:    limits.NewConnectionRateLimitPeriod,
		globalCapacity: limits.GlobalNewConnectionRateLimitCapacity,
		globalPeriod:   limits.GlobalNewConnectionRateLimitPeriod,
		maxEntries:     limits.MaxConnectionRateLimiterEntries,
		perIP:          make(map[remoteAddressKey]rateLimitState),
	}
}

func (l *newConnectionRateLimiter) allow(addr net.Addr) error {
	return l.allowKeyAt(remoteIPKey(addr), time.Now())
}

func (l *newConnectionRateLimiter) allowAt(ip string, now time.Time) error {
	return l.allowKeyAt(remoteAddressKey{fallback: ip}, now)
}

func (l *newConnectionRateLimiter) allowKeyAt(ip remoteAddressKey, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanup(now)

	state, exists := l.perIP[ip]
	if !exists && len(l.perIP) >= l.maxEntries {
		return errConnectionRateLimiterFull
	}
	if !state.take(now, l.perIPCapacity, l.perIPPeriod) {
		l.scheduleCleanup(now)
		return errNewConnectionRateLimited
	}
	if !l.global.take(now, l.globalCapacity, l.globalPeriod) {
		return errGlobalNewConnectionRateLimited
	}

	l.perIP[ip] = state
	l.scheduleCleanup(now)
	return nil
}

func (l *newConnectionRateLimiter) cleanup(now time.Time) {
	if l.cleanupAt.IsZero() || now.Before(l.cleanupAt) {
		return
	}

	for ip, state := range l.perIP {
		if state.full(now, l.perIPCapacity, l.perIPPeriod) {
			delete(l.perIP, ip)
		}
	}
	if len(l.perIP) == 0 {
		l.cleanupAt = time.Time{}
	} else {
		l.cleanupAt = now.Add(connectionRateLimiterCleanupPeriod)
	}
}

func (l *newConnectionRateLimiter) scheduleCleanup(now time.Time) {
	if l.cleanupAt.IsZero() {
		l.cleanupAt = now.Add(connectionRateLimiterCleanupPeriod)
	}
}
