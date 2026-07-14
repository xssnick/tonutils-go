package adnl

import (
	"sync/atomic"
	"time"
)

// PeerStats is a point-in-time snapshot of an ADNL peer state.
// Counters are cumulative for the lifetime of the peer and are not reset by Reinit.
type PeerStats struct {
	CapturedAt       time.Time
	CreatedAt        time.Time
	Closed           bool
	Channel          PeerChannelStats
	Inbound          PeerInboundStats
	Outbound         PeerOutboundStats
	Sequences        PeerSequenceStats
	Reinitialization PeerReinitializationStats
}

// PeerInboundStats describes authenticated ADNL datagrams attributed to the peer.
type PeerInboundStats struct {
	Packets          uint64
	Bytes            uint64
	ProcessingErrors uint64
	LastPacketAt     time.Time
	LastErrorAt      time.Time
}

// PeerOutboundStats describes ADNL datagrams accepted by the packet writer.
type PeerOutboundStats struct {
	Packets      uint64
	Bytes        uint64
	WriteErrors  uint64
	LastPacketAt time.Time
	LastErrorAt  time.Time
}

// PeerSequenceStats contains the current ADNL sequence and confirmation state.
type PeerSequenceStats struct {
	// LocalSeqno is the highest locally allocated ADNL sequence number.
	LocalSeqno int64
	// PeerSeqno is the highest ADNL sequence number received from the peer.
	PeerSeqno int64
	// ConfirmedByPeerSeqno is the highest local sequence number reported by the peer.
	ConfirmedByPeerSeqno int64
}

// PeerChannelStats describes the encrypted ADNL channel state.
type PeerChannelStats struct {
	State PeerChannelState
}

// PeerChannelState is the current channel negotiation state.
type PeerChannelState uint8

const (
	PeerChannelStateUnknown PeerChannelState = iota
	PeerChannelStateNone
	PeerChannelStatePending
	PeerChannelStateReady
)

// PeerReinitializationStats contains current protocol epochs and cumulative recovery events.
type PeerReinitializationStats struct {
	LocalEpoch           int32
	PeerEpoch            int32
	PeerEvents           uint64
	RootRecoveryAttempts uint64
	LastPeerEventAt      time.Time
	LastRootRecoveryAt   time.Time
}

type peerStats struct {
	createdAt int64

	inboundPackets          atomic.Uint64
	inboundBytes            atomic.Uint64
	inboundProcessingErrors atomic.Uint64
	inboundLastErrorAt      atomic.Int64

	outboundPackets      atomic.Uint64
	outboundBytes        atomic.Uint64
	outboundWriteErrors  atomic.Uint64
	outboundLastPacketAt atomic.Int64
	outboundLastErrorAt  atomic.Int64

	peerConfirmSeqno atomic.Int64

	peerReinitializations atomic.Uint64
	rootRecoveryAttempts  atomic.Uint64
	lastPeerReinitAt      atomic.Int64
	lastRootRecoveryAt    atomic.Int64
}

func newPeerStats(now time.Time) *peerStats {
	return &peerStats{createdAt: now.UnixNano()}
}

func (a *ADNL) Stats() PeerStats {
	channelState := PeerChannelStateNone
	if channel := (*Channel)(atomic.LoadPointer(&a.channelPtr)); channel != nil {
		channelState = PeerChannelStatePending
		if channel.ready.Load() {
			channelState = PeerChannelStateReady
		}
	}

	stats := PeerStats{
		CreatedAt: timeFromUnixNano(a.stats.createdAt),
		Closed:    a.closerCtx.Err() != nil,
		Channel: PeerChannelStats{
			State: channelState,
		},
		Inbound: PeerInboundStats{
			Packets:          a.stats.inboundPackets.Load(),
			Bytes:            a.stats.inboundBytes.Load(),
			ProcessingErrors: a.stats.inboundProcessingErrors.Load(),
			LastPacketAt:     timeFromUnixNano(atomic.LoadInt64(&a.lastReceivedPacket)),
			LastErrorAt:      timeFromUnixNano(a.stats.inboundLastErrorAt.Load()),
		},
		Outbound: PeerOutboundStats{
			Packets:      a.stats.outboundPackets.Load(),
			Bytes:        a.stats.outboundBytes.Load(),
			WriteErrors:  a.stats.outboundWriteErrors.Load(),
			LastPacketAt: timeFromUnixNano(a.stats.outboundLastPacketAt.Load()),
			LastErrorAt:  timeFromUnixNano(a.stats.outboundLastErrorAt.Load()),
		},
		Sequences: PeerSequenceStats{
			LocalSeqno:           atomic.LoadInt64(&a.seqno),
			PeerSeqno:            atomic.LoadInt64(&a.confirmSeqno),
			ConfirmedByPeerSeqno: a.stats.peerConfirmSeqno.Load(),
		},
		Reinitialization: PeerReinitializationStats{
			LocalEpoch:           atomic.LoadInt32(&a.reinitTime),
			PeerEpoch:            atomic.LoadInt32(&a.dstReinit),
			PeerEvents:           a.stats.peerReinitializations.Load(),
			RootRecoveryAttempts: a.stats.rootRecoveryAttempts.Load(),
			LastPeerEventAt:      timeFromUnixNano(a.stats.lastPeerReinitAt.Load()),
			LastRootRecoveryAt:   timeFromUnixNano(a.stats.lastRootRecoveryAt.Load()),
		},
	}
	stats.CapturedAt = time.Now()
	return stats
}

func (a *ADNL) noteInboundPacket(size int) {
	a.stats.noteInboundPacket(size)
}

func (a *ADNL) noteInboundError(now time.Time) {
	a.stats.noteInboundError(now)
}

func (s *peerStats) noteInboundPacket(size int) {
	s.inboundPackets.Add(1)
	s.inboundBytes.Add(uint64(size))
}

func (s *peerStats) noteInboundError(now time.Time) {
	s.inboundProcessingErrors.Add(1)
	storeLatestTime(&s.inboundLastErrorAt, now)
}

func (s *peerStats) noteOutboundPacket(size int, now time.Time) {
	s.outboundPackets.Add(1)
	s.outboundBytes.Add(uint64(size))
	storeLatestTime(&s.outboundLastPacketAt, now)
}

func (s *peerStats) noteOutboundError(now time.Time) {
	s.outboundWriteErrors.Add(1)
	storeLatestTime(&s.outboundLastErrorAt, now)
}

func (s *peerStats) notePeerReinit(now time.Time) {
	s.peerReinitializations.Add(1)
	storeLatestTime(&s.lastPeerReinitAt, now)
}

func (s *peerStats) noteRootRecovery(now time.Time) {
	s.rootRecoveryAttempts.Add(1)
	storeLatestTime(&s.lastRootRecoveryAt, now)
}

func storeLatestTime(dst *atomic.Int64, value time.Time) {
	next := value.UnixNano()
	for {
		current := dst.Load()
		if current >= next {
			return
		}
		if dst.CompareAndSwap(current, next) {
			return
		}
	}
}

func timeFromUnixNano(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}

	return time.Unix(0, value)
}
