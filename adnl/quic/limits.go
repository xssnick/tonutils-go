package quic

import (
	"fmt"
	"math"
	"time"
)

const (
	defaultMaxIncomingStreams                        = int64(1024)
	defaultMaxConcurrentIncomingStreams              = 256
	defaultMaxConcurrentIncomingStreamsPerConnection = 32
	// The per-connection budget leaves room for two concurrent max-size
	// objects so one in-flight near-max broadcast does not silently drop a
	// second on the same connection; the global budget keeps a 4x ratio over
	// it so a few connections cannot exhaust it.
	defaultMaxBufferedIncomingBytes              = 8 * int64(DefaultMaxObjectSize)
	defaultMaxBufferedIncomingBytesPerConnection = 2 * int64(DefaultMaxObjectSize)
	defaultStreamReadTimeout                     = 15 * time.Second
	defaultMaxConnections                        = 4096
	defaultMaxPeerPaths                          = 4096
	defaultNewConnectionRateLimitCapacity        = 10
	defaultNewConnectionRateLimitPeriod          = 200 * time.Millisecond
	defaultGlobalNewConnectionRateLimitCapacity  = 100000
	defaultGlobalNewConnectionRateLimitPeriod    = 10 * time.Microsecond
	defaultMaxConnectionRateLimiterEntries       = 16384
)

// Limits bounds QUIC protocol and application resources for one Server or
// Gateway. MaxIncomingStreams keeps quic-go semantics: zero selects its
// default and a negative value disables peer-initiated bidirectional streams.
// Every other field must be positive.
type Limits struct {
	// MaxObjectSize is the default whole-stream cap. Client.Query may override
	// it for that response stream with a positive maxAnswer.
	MaxObjectSize                             int64
	MaxIncomingStreams                        int64
	MaxConcurrentIncomingStreams              int
	MaxConcurrentIncomingStreamsPerConnection int
	MaxBufferedIncomingBytes                  int64
	MaxBufferedIncomingBytesPerConnection     int64
	// StreamReadTimeout bounds the lifetime of an incomplete untrusted object.
	// This intentionally hardens the C++ behavior, where keepalives can keep a
	// partially written stream alive indefinitely.
	StreamReadTimeout time.Duration
	// MaxConnections and MaxConnectionsPerIP bound accepted inbound
	// connections. Locally initiated connections remain bounded by MaxPeerPaths.
	MaxConnections                       int
	MaxConnectionsPerIP                  int
	NewConnectionRateLimitCapacity       int
	NewConnectionRateLimitPeriod         time.Duration
	GlobalNewConnectionRateLimitCapacity int
	GlobalNewConnectionRateLimitPeriod   time.Duration
	MaxConnectionRateLimiterEntries      int
	MaxPeerPaths                         int
}

// DefaultLimits returns the production defaults used by TON QUIC.
func DefaultLimits() Limits {
	return Limits{
		MaxObjectSize:                             DefaultMaxObjectSize,
		MaxIncomingStreams:                        defaultMaxIncomingStreams,
		MaxConcurrentIncomingStreams:              defaultMaxConcurrentIncomingStreams,
		MaxConcurrentIncomingStreamsPerConnection: defaultMaxConcurrentIncomingStreamsPerConnection,
		MaxBufferedIncomingBytes:                  defaultMaxBufferedIncomingBytes,
		MaxBufferedIncomingBytesPerConnection:     defaultMaxBufferedIncomingBytesPerConnection,
		StreamReadTimeout:                         defaultStreamReadTimeout,
		MaxConnections:                            defaultMaxConnections,
		MaxConnectionsPerIP:                       defaultMaxConnectionsPerIP,
		NewConnectionRateLimitCapacity:            defaultNewConnectionRateLimitCapacity,
		NewConnectionRateLimitPeriod:              defaultNewConnectionRateLimitPeriod,
		GlobalNewConnectionRateLimitCapacity:      defaultGlobalNewConnectionRateLimitCapacity,
		GlobalNewConnectionRateLimitPeriod:        defaultGlobalNewConnectionRateLimitPeriod,
		MaxConnectionRateLimiterEntries:           defaultMaxConnectionRateLimiterEntries,
		MaxPeerPaths:                              defaultMaxPeerPaths,
	}
}

func (l Limits) validate() error {
	if l.MaxObjectSize <= 0 {
		return fmt.Errorf("quic: MaxObjectSize must be positive")
	}
	maxObjectSize := maxBoxedObjectWireSize
	if maxInt := int64(^uint(0) >> 1); maxInt < maxObjectSize {
		maxObjectSize = maxInt
	}
	if l.MaxObjectSize > maxObjectSize {
		return fmt.Errorf("quic: MaxObjectSize exceeds protocol or platform limit")
	}
	if l.MaxConcurrentIncomingStreams <= 0 {
		return fmt.Errorf("quic: MaxConcurrentIncomingStreams must be positive")
	}
	if l.MaxConcurrentIncomingStreamsPerConnection <= 0 {
		return fmt.Errorf("quic: MaxConcurrentIncomingStreamsPerConnection must be positive")
	}
	if l.MaxConcurrentIncomingStreamsPerConnection > l.MaxConcurrentIncomingStreams {
		return fmt.Errorf("quic: MaxConcurrentIncomingStreamsPerConnection exceeds global limit")
	}
	if l.MaxBufferedIncomingBytes <= 0 {
		return fmt.Errorf("quic: MaxBufferedIncomingBytes must be positive")
	}
	if l.MaxBufferedIncomingBytesPerConnection <= 0 {
		return fmt.Errorf("quic: MaxBufferedIncomingBytesPerConnection must be positive")
	}
	if l.MaxBufferedIncomingBytesPerConnection > l.MaxBufferedIncomingBytes {
		return fmt.Errorf("quic: MaxBufferedIncomingBytesPerConnection exceeds global limit")
	}
	if l.MaxObjectSize > l.MaxBufferedIncomingBytes {
		return fmt.Errorf("quic: MaxObjectSize exceeds global buffered byte limit")
	}
	if l.MaxObjectSize > l.MaxBufferedIncomingBytesPerConnection {
		return fmt.Errorf("quic: MaxObjectSize exceeds per-connection buffered byte limit")
	}
	if l.StreamReadTimeout <= 0 {
		return fmt.Errorf("quic: StreamReadTimeout must be positive")
	}
	if l.MaxConnections <= 0 {
		return fmt.Errorf("quic: MaxConnections must be positive")
	}
	if l.MaxConnectionsPerIP <= 0 {
		return fmt.Errorf("quic: MaxConnectionsPerIP must be positive")
	}
	if l.NewConnectionRateLimitCapacity <= 0 {
		return fmt.Errorf("quic: NewConnectionRateLimitCapacity must be positive")
	}
	if l.NewConnectionRateLimitPeriod <= 0 {
		return fmt.Errorf("quic: NewConnectionRateLimitPeriod must be positive")
	}
	if rateLimitDurationOverflows(l.NewConnectionRateLimitCapacity, l.NewConnectionRateLimitPeriod) {
		return fmt.Errorf("quic: per-IP new connection rate limit duration overflows")
	}
	if l.GlobalNewConnectionRateLimitCapacity <= 0 {
		return fmt.Errorf("quic: GlobalNewConnectionRateLimitCapacity must be positive")
	}
	if l.GlobalNewConnectionRateLimitPeriod <= 0 {
		return fmt.Errorf("quic: GlobalNewConnectionRateLimitPeriod must be positive")
	}
	if rateLimitDurationOverflows(
		l.GlobalNewConnectionRateLimitCapacity,
		l.GlobalNewConnectionRateLimitPeriod,
	) {
		return fmt.Errorf("quic: global new connection rate limit duration overflows")
	}
	if l.MaxConnectionRateLimiterEntries <= 0 {
		return fmt.Errorf("quic: MaxConnectionRateLimiterEntries must be positive")
	}
	if l.MaxPeerPaths <= 0 {
		return fmt.Errorf("quic: MaxPeerPaths must be positive")
	}
	return nil
}

func rateLimitDurationOverflows(capacity int, period time.Duration) bool {
	if capacity <= 1 {
		return false
	}
	return uint64(capacity-1) > uint64(math.MaxInt64)/uint64(period)
}
