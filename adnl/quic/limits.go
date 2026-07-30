package quic

import (
	"errors"
	"math"
	"time"
)

const (
	// QuicConnectionOptions::DEFAULT_MAX_STREAMS_BIDI (quic-pimpl.h). Anything
	// lower is tighter than the C++ node advertises.
	defaultMaxIncomingStreams = int64(4096)
	// Concurrent application-level stream processing. The per-connection number is
	// the real gate; the receiver-wide one is a burst pool on top of it.
	defaultMaxConcurrentIncomingStreams              = 4096
	defaultMaxConcurrentIncomingStreamsPerConnection = 64
	// Streams every connection may serve without touching the shared pool, so
	// no set of peers can starve another connection down to zero.
	defaultGuaranteedStreamsPerConnection = 2
	// Buffered payload budgets, counting bytes that ARRIVED rather than bytes a
	// peer claimed it would send, so they can be generous without handing out an
	// amplification primitive. The reference node has no global budget at all.
	defaultMaxBufferedIncomingBytes              = 32 * int64(DefaultMaxObjectSize)
	defaultMaxBufferedIncomingBytesPerConnection = 4 * int64(DefaultMaxObjectSize)
	// Idle, not absolute: see readIncomingBoxedObject.
	defaultStreamReadTimeout  = 15 * time.Second
	defaultStreamTotalTimeout = 0
	defaultMaxConnections     = 4096
	defaultMaxPeerPaths       = 4096
	// The slice of MaxPeerPaths inbound connections may never occupy: both
	// directions share one table, so without a reserve an inbound flood exhausts
	// it and the node can no longer dial out.
	defaultOutboundPathReserve                  = 512
	defaultNewConnectionRateLimitCapacity       = 10
	defaultNewConnectionRateLimitPeriod         = 200 * time.Millisecond
	defaultGlobalNewConnectionRateLimitCapacity = 100000
	defaultGlobalNewConnectionRateLimitPeriod   = 10 * time.Microsecond
	defaultMaxConnectionRateLimiterEntries      = 16384
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
	// GuaranteedStreamsPerConnection is how many concurrent inbound streams a
	// connection may serve without consuming a slot from the receiver-wide
	// pool. Zero selects the default.
	GuaranteedStreamsPerConnection int
	// StreamReadTimeout is an IDLE bound, refreshed whenever the stream makes
	// progress: "this peer went quiet", not "this object took too long". An
	// absolute budget would be an implicit minimum bandwidth. The reference node
	// puts no timeout on inbound streams at all, so this is still a hardening.
	StreamReadTimeout time.Duration
	// StreamTotalTimeout optionally caps the whole stream regardless of progress.
	// Zero, the default, means no ceiling, matching C++.
	StreamTotalTimeout time.Duration
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
	// OutboundPathReserve bounds inbound peer paths to
	// MaxPeerPaths-OutboundPathReserve, keeping the remainder available for
	// locally initiated paths regardless of inbound pressure.
	OutboundPathReserve int
}

// Clamped to the per-connection cap so lowering only that cap stays valid.
func (l Limits) guaranteedStreamsPerConnection() int {
	guaranteed := l.GuaranteedStreamsPerConnection
	if guaranteed <= 0 {
		guaranteed = defaultGuaranteedStreamsPerConnection
	}
	if guaranteed > l.MaxConcurrentIncomingStreamsPerConnection {
		guaranteed = l.MaxConcurrentIncomingStreamsPerConnection
	}
	return guaranteed
}

// StreamWriteTimeout is the idle bound applied to writing an answer. It tracks
// StreamReadTimeout so there is one knob for "peer went quiet".
func (l Limits) StreamWriteTimeout() time.Duration {
	return l.StreamReadTimeout
}

// The reserve is clamped to half the table so lowering MaxPeerPaths alone stays
// valid and inbound is never squeezed out entirely.
func (l Limits) inboundPeerPathBudget() int {
	reserve := l.OutboundPathReserve
	if maxReserve := l.MaxPeerPaths / 2; reserve > maxReserve {
		reserve = maxReserve
	}
	return l.MaxPeerPaths - reserve
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
		GuaranteedStreamsPerConnection:            defaultGuaranteedStreamsPerConnection,
		StreamReadTimeout:                         defaultStreamReadTimeout,
		StreamTotalTimeout:                        defaultStreamTotalTimeout,
		MaxConnections:                            defaultMaxConnections,
		MaxConnectionsPerIP:                       defaultMaxConnectionsPerIP,
		NewConnectionRateLimitCapacity:            defaultNewConnectionRateLimitCapacity,
		NewConnectionRateLimitPeriod:              defaultNewConnectionRateLimitPeriod,
		GlobalNewConnectionRateLimitCapacity:      defaultGlobalNewConnectionRateLimitCapacity,
		GlobalNewConnectionRateLimitPeriod:        defaultGlobalNewConnectionRateLimitPeriod,
		MaxConnectionRateLimiterEntries:           defaultMaxConnectionRateLimiterEntries,
		MaxPeerPaths:                              defaultMaxPeerPaths,
		OutboundPathReserve:                       defaultOutboundPathReserve,
	}
}

func (l Limits) validate() error {
	if l.MaxObjectSize <= 0 {
		return errors.New("quic: MaxObjectSize must be positive")
	}
	maxObjectSize := maxBoxedObjectWireSize
	if maxInt := int64(^uint(0) >> 1); maxInt < maxObjectSize {
		maxObjectSize = maxInt
	}
	if l.MaxObjectSize > maxObjectSize {
		return errors.New("quic: MaxObjectSize exceeds protocol or platform limit")
	}
	if l.MaxConcurrentIncomingStreams <= 0 {
		return errors.New("quic: MaxConcurrentIncomingStreams must be positive")
	}
	if l.MaxConcurrentIncomingStreamsPerConnection <= 0 {
		return errors.New("quic: MaxConcurrentIncomingStreamsPerConnection must be positive")
	}
	if l.MaxConcurrentIncomingStreamsPerConnection > l.MaxConcurrentIncomingStreams {
		return errors.New("quic: MaxConcurrentIncomingStreamsPerConnection exceeds global limit")
	}
	if l.MaxBufferedIncomingBytes <= 0 {
		return errors.New("quic: MaxBufferedIncomingBytes must be positive")
	}
	if l.MaxBufferedIncomingBytesPerConnection <= 0 {
		return errors.New("quic: MaxBufferedIncomingBytesPerConnection must be positive")
	}
	if l.MaxBufferedIncomingBytesPerConnection > l.MaxBufferedIncomingBytes {
		return errors.New("quic: MaxBufferedIncomingBytesPerConnection exceeds global limit")
	}
	if l.MaxObjectSize > l.MaxBufferedIncomingBytes {
		return errors.New("quic: MaxObjectSize exceeds global buffered byte limit")
	}
	if l.MaxObjectSize > l.MaxBufferedIncomingBytesPerConnection {
		return errors.New("quic: MaxObjectSize exceeds per-connection buffered byte limit")
	}
	if l.StreamReadTimeout <= 0 {
		return errors.New("quic: StreamReadTimeout must be positive")
	}
	if l.StreamTotalTimeout < 0 {
		return errors.New("quic: StreamTotalTimeout cannot be negative")
	}
	if l.StreamTotalTimeout > 0 && l.StreamTotalTimeout < l.StreamReadTimeout {
		return errors.New("quic: StreamTotalTimeout must not be shorter than StreamReadTimeout")
	}
	if l.GuaranteedStreamsPerConnection < 0 {
		return errors.New("quic: GuaranteedStreamsPerConnection cannot be negative")
	}
	if l.MaxConnections <= 0 {
		return errors.New("quic: MaxConnections must be positive")
	}
	if l.MaxConnectionsPerIP <= 0 {
		return errors.New("quic: MaxConnectionsPerIP must be positive")
	}
	if l.NewConnectionRateLimitCapacity <= 0 {
		return errors.New("quic: NewConnectionRateLimitCapacity must be positive")
	}
	if l.NewConnectionRateLimitPeriod <= 0 {
		return errors.New("quic: NewConnectionRateLimitPeriod must be positive")
	}
	if rateLimitDurationOverflows(l.NewConnectionRateLimitCapacity, l.NewConnectionRateLimitPeriod) {
		return errors.New("quic: per-IP new connection rate limit duration overflows")
	}
	if l.GlobalNewConnectionRateLimitCapacity <= 0 {
		return errors.New("quic: GlobalNewConnectionRateLimitCapacity must be positive")
	}
	if l.GlobalNewConnectionRateLimitPeriod <= 0 {
		return errors.New("quic: GlobalNewConnectionRateLimitPeriod must be positive")
	}
	if rateLimitDurationOverflows(
		l.GlobalNewConnectionRateLimitCapacity,
		l.GlobalNewConnectionRateLimitPeriod,
	) {
		return errors.New("quic: global new connection rate limit duration overflows")
	}
	if l.MaxConnectionRateLimiterEntries <= 0 {
		return errors.New("quic: MaxConnectionRateLimiterEntries must be positive")
	}
	if l.MaxPeerPaths <= 0 {
		return errors.New("quic: MaxPeerPaths must be positive")
	}
	if l.OutboundPathReserve < 0 {
		return errors.New("quic: OutboundPathReserve cannot be negative")
	}
	return nil
}

func rateLimitDurationOverflows(capacity int, period time.Duration) bool {
	if capacity <= 1 {
		return false
	}
	return uint64(capacity-1) > uint64(math.MaxInt64)/uint64(period)
}
