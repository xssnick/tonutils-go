package quic

import (
	"errors"
	"testing"
	"time"
)

func TestNewConnectionRateLimiterPerIPBurstAndRefill(t *testing.T) {
	limits := DefaultLimits()
	limits.NewConnectionRateLimitCapacity = 3
	limits.NewConnectionRateLimitPeriod = 100 * time.Millisecond
	limiter := newConnectionRateLimits(limits)
	now := time.Unix(1, 0)

	for i := 0; i < 3; i++ {
		if err := limiter.allowAt("192.0.2.1", now); err != nil {
			t.Fatalf("burst connection %d: %v", i, err)
		}
	}
	if err := limiter.allowAt("192.0.2.1", now); !errors.Is(err, errNewConnectionRateLimited) {
		t.Fatalf("overflow error = %v, want %v", err, errNewConnectionRateLimited)
	}
	if err := limiter.allowAt("192.0.2.1", now.Add(100*time.Millisecond)); err != nil {
		t.Fatalf("connection after refill: %v", err)
	}
	if err := limiter.allowAt("192.0.2.2", now); err != nil {
		t.Fatalf("independent source IP: %v", err)
	}
}

func TestNewConnectionRateLimiterGlobalBurst(t *testing.T) {
	limits := DefaultLimits()
	limits.GlobalNewConnectionRateLimitCapacity = 2
	limits.GlobalNewConnectionRateLimitPeriod = time.Second
	limiter := newConnectionRateLimits(limits)
	now := time.Unix(2, 0)

	if err := limiter.allowAt("192.0.2.1", now); err != nil {
		t.Fatal(err)
	}
	if err := limiter.allowAt("192.0.2.2", now); err != nil {
		t.Fatal(err)
	}
	if err := limiter.allowAt("192.0.2.3", now); !errors.Is(err, errGlobalNewConnectionRateLimited) {
		t.Fatalf("overflow error = %v, want %v", err, errGlobalNewConnectionRateLimited)
	}
}

func TestNewConnectionRateLimiterCleansRefilledSources(t *testing.T) {
	limits := DefaultLimits()
	limiter := newConnectionRateLimits(limits)
	now := time.Unix(3, 0)

	if err := limiter.allowAt("192.0.2.1", now); err != nil {
		t.Fatal(err)
	}
	if len(limiter.perIP) != 1 {
		t.Fatalf("source entries = %d, want 1", len(limiter.perIP))
	}

	afterCleanup := now.Add(connectionRateLimiterCleanupPeriod + time.Second)
	if err := limiter.allowAt("192.0.2.2", afterCleanup); err != nil {
		t.Fatal(err)
	}
	if _, ok := limiter.perIP[remoteAddressKey{fallback: "192.0.2.1"}]; ok {
		t.Fatal("fully refilled source was not removed")
	}
}

func TestNewConnectionRateLimiterBoundsSourceTable(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxConnectionRateLimiterEntries = 2
	limiter := newConnectionRateLimits(limits)
	now := time.Unix(4, 0)

	if err := limiter.allowAt("192.0.2.1", now); err != nil {
		t.Fatal(err)
	}
	if err := limiter.allowAt("192.0.2.2", now); err != nil {
		t.Fatal(err)
	}
	if err := limiter.allowAt("192.0.2.3", now); !errors.Is(err, errConnectionRateLimiterFull) {
		t.Fatalf("overflow error = %v, want %v", err, errConnectionRateLimiterFull)
	}
	if len(limiter.perIP) != limits.MaxConnectionRateLimiterEntries {
		t.Fatalf("source entries = %d, want %d", len(limiter.perIP), limits.MaxConnectionRateLimiterEntries)
	}
}

func TestGlobalRateRejectionDoesNotGrowSourceTable(t *testing.T) {
	limits := DefaultLimits()
	limits.GlobalNewConnectionRateLimitCapacity = 1
	limits.GlobalNewConnectionRateLimitPeriod = time.Second
	limiter := newConnectionRateLimits(limits)
	now := time.Unix(5, 0)

	if err := limiter.allowAt("192.0.2.1", now); err != nil {
		t.Fatal(err)
	}
	if err := limiter.allowAt("192.0.2.2", now); !errors.Is(err, errGlobalNewConnectionRateLimited) {
		t.Fatalf("global rejection error = %v, want %v", err, errGlobalNewConnectionRateLimited)
	}
	if len(limiter.perIP) != 1 {
		t.Fatalf("source entries after global rejection = %d, want 1", len(limiter.perIP))
	}
}
