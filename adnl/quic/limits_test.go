package quic

import (
	"testing"
)

func TestDefaultLimitsAreValid(t *testing.T) {
	limits := DefaultLimits()
	if err := limits.validate(); err != nil {
		t.Fatalf("default limits: %v", err)
	}
	if limits.MaxObjectSize != DefaultMaxObjectSize {
		t.Fatalf("MaxObjectSize = %d, want %d", limits.MaxObjectSize, DefaultMaxObjectSize)
	}
	if limits.MaxIncomingStreams != defaultMaxIncomingStreams {
		t.Fatalf("MaxIncomingStreams = %d, want %d", limits.MaxIncomingStreams, defaultMaxIncomingStreams)
	}
	if limits.MaxConnections != DefaultMaxConnections {
		t.Fatalf("MaxConnections = %d, want %d", limits.MaxConnections, DefaultMaxConnections)
	}
	if limits.MaxConnectionsPerIP != DefaultMaxConnectionsPerIP {
		t.Fatalf("MaxConnectionsPerIP = %d, want %d", limits.MaxConnectionsPerIP, DefaultMaxConnectionsPerIP)
	}
	if limits.MaxPeerPaths != DefaultMaxPeerPaths {
		t.Fatalf("MaxPeerPaths = %d, want %d", limits.MaxPeerPaths, DefaultMaxPeerPaths)
	}
	if limits.OutboundPathReserve != DefaultOutboundPathReserve {
		t.Fatalf("OutboundPathReserve = %d, want %d", limits.OutboundPathReserve, DefaultOutboundPathReserve)
	}
	// The per-connection cap is the gate that actually backpressures a peer, so
	// it is what must stay below the advertised QUIC credit. The receiver-wide
	// number is a burst pool layered on top of the per-connection guarantee and
	// is deliberately sized not to bind before the protocol limit does -- the
	// reference node has no receiver-wide application cap at all.
	if limits.MaxConcurrentIncomingStreamsPerConnection >= int(limits.MaxIncomingStreams) {
		t.Fatalf(
			"per-connection stream limit %d must be below protocol credit %d",
			limits.MaxConcurrentIncomingStreamsPerConnection,
			limits.MaxIncomingStreams,
		)
	}
	if limits.MaxConcurrentIncomingStreams > int(limits.MaxIncomingStreams) {
		t.Fatalf(
			"application stream pool %d must not exceed protocol credit %d",
			limits.MaxConcurrentIncomingStreams,
			limits.MaxIncomingStreams,
		)
	}
	if limits.GuaranteedStreamsPerConnection <= 0 ||
		limits.GuaranteedStreamsPerConnection > limits.MaxConcurrentIncomingStreamsPerConnection {
		t.Fatalf(
			"guaranteed streams %d must be within (0, %d]",
			limits.GuaranteedStreamsPerConnection,
			limits.MaxConcurrentIncomingStreamsPerConnection,
		)
	}
	if limits.MaxBufferedIncomingBytes < limits.MaxObjectSize {
		t.Fatalf(
			"buffered byte limit %d is below object limit %d",
			limits.MaxBufferedIncomingBytes,
			limits.MaxObjectSize,
		)
	}
	if limits.MaxBufferedIncomingBytesPerConnection > limits.MaxBufferedIncomingBytes {
		t.Fatalf(
			"per-connection byte limit %d exceeds global limit %d",
			limits.MaxBufferedIncomingBytesPerConnection,
			limits.MaxBufferedIncomingBytes,
		)
	}
	if limits.MaxBufferedIncomingBytesPerConnection < limits.MaxObjectSize {
		t.Fatalf(
			"per-connection byte limit %d is below object limit %d",
			limits.MaxBufferedIncomingBytesPerConnection,
			limits.MaxObjectSize,
		)
	}
}

func TestLimitsAllowObjectSizeAbovePlumtreeDefault(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxObjectSize = 32 << 20
	limits.MaxBufferedIncomingBytesPerConnection = 32 << 20

	if err := limits.validate(); err != nil {
		t.Fatalf("32 MiB object limit: %v", err)
	}
}

func TestLimitsRejectInvalidFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Limits)
	}{
		{
			name:   "object size",
			mutate: func(l *Limits) { l.MaxObjectSize = 0 },
		},
		{
			name:   "object size over TL limit",
			mutate: func(l *Limits) { l.MaxObjectSize = maxBoxedObjectWireSize + 1 },
		},
		{
			name:   "active streams",
			mutate: func(l *Limits) { l.MaxConcurrentIncomingStreams = 0 },
		},
		{
			name:   "active streams per connection",
			mutate: func(l *Limits) { l.MaxConcurrentIncomingStreamsPerConnection = 0 },
		},
		{
			name: "active streams per connection over global",
			mutate: func(l *Limits) {
				l.MaxConcurrentIncomingStreamsPerConnection = l.MaxConcurrentIncomingStreams + 1
			},
		},
		{
			name:   "buffered bytes",
			mutate: func(l *Limits) { l.MaxBufferedIncomingBytes = 0 },
		},
		{
			name:   "buffered bytes per connection",
			mutate: func(l *Limits) { l.MaxBufferedIncomingBytesPerConnection = 0 },
		},
		{
			name: "buffered bytes per connection over global",
			mutate: func(l *Limits) {
				l.MaxBufferedIncomingBytesPerConnection = l.MaxBufferedIncomingBytes + 1
			},
		},
		{
			name: "object size over global buffered bytes",
			mutate: func(l *Limits) {
				l.MaxBufferedIncomingBytes = l.MaxObjectSize - 1
			},
		},
		{
			name: "object size over per-connection buffered bytes",
			mutate: func(l *Limits) {
				l.MaxBufferedIncomingBytesPerConnection = l.MaxObjectSize - 1
			},
		},
		{
			name:   "stream read timeout",
			mutate: func(l *Limits) { l.StreamReadTimeout = 0 },
		},
		{
			name:   "connections",
			mutate: func(l *Limits) { l.MaxConnections = 0 },
		},
		{
			name:   "connections per IP",
			mutate: func(l *Limits) { l.MaxConnectionsPerIP = 0 },
		},
		{
			name:   "per-IP connection rate capacity",
			mutate: func(l *Limits) { l.NewConnectionRateLimitCapacity = 0 },
		},
		{
			name:   "per-IP connection rate period",
			mutate: func(l *Limits) { l.NewConnectionRateLimitPeriod = 0 },
		},
		{
			name:   "global connection rate capacity",
			mutate: func(l *Limits) { l.GlobalNewConnectionRateLimitCapacity = 0 },
		},
		{
			name:   "global connection rate period",
			mutate: func(l *Limits) { l.GlobalNewConnectionRateLimitPeriod = 0 },
		},
		{
			name:   "connection rate source entries",
			mutate: func(l *Limits) { l.MaxConnectionRateLimiterEntries = 0 },
		},
		{
			name:   "peer paths",
			mutate: func(l *Limits) { l.MaxPeerPaths = 0 },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			test.mutate(&limits)
			if err := limits.validate(); err == nil {
				t.Fatal("invalid limits were accepted")
			}
		})
	}
}
