package rldp

import (
	"sync/atomic"
	"time"
)

// Stats is a point-in-time snapshot of an RLDP peer state.
// Counters are cumulative for the lifetime of the client. Fields are sampled
// independently, so active counts and terminal counters may briefly differ
// while a transfer is being finalized.
type Stats struct {
	CapturedAt time.Time
	CreatedAt  time.Time
	Closed     bool
	Version    Version
	Active     ActiveStats
	Inbound    InboundStats
	Outbound   OutboundStats
	Congestion CongestionStats
}

// Version identifies the currently negotiated RLDP protocol version.
type Version uint8

const (
	VersionUnknown Version = iota
	Version1
	Version2
)

// ActiveStats contains current request and transfer counts.
// InboundStreams includes completed streams retained for duplicate completion handling.
type ActiveStats struct {
	Requests          int
	OutboundTransfers int
	InboundStreams    int
}

// OutboundStats describes transfers and symbols sent to the peer.
type OutboundStats struct {
	TransfersStarted   uint64
	TransfersCompleted uint64
	TransfersTimedOut  uint64
	TransfersFailed    uint64
	TransfersCanceled  uint64

	SymbolsSent     uint64
	SymbolBytesSent uint64

	ConfirmationsReceived uint64
	ConfirmedSymbols      uint64

	LastSymbolAt       time.Time
	LastConfirmationAt time.Time
}

// InboundStats describes transfers and symbols received from the peer.
type InboundStats struct {
	TransfersStarted   uint64
	TransfersCompleted uint64
	TransfersExpired   uint64
	TransfersCanceled  uint64

	// Symbol counters are committed when a part completes or an unfinished stream is retired.
	// This keeps atomic operations out of the per-symbol receive path.
	SymbolsReceived       uint64
	SymbolBytesReceived   uint64
	RepairSymbolsReceived uint64
	PayloadBytesDecoded   uint64
	ProcessingErrors      uint64

	ConfirmationsSent      uint64
	ConfirmationSendErrors uint64

	LastSymbolAt       time.Time
	LastCompleteAt     time.Time
	LastConfirmationAt time.Time
}

// CongestionStats is a snapshot of the RLDPv2 congestion controller.
type CongestionStats struct {
	State CongestionState

	RTTObserved bool
	LatestRTT   time.Duration
	MinRTT      time.Duration

	DeliveryRateBytesPerSecond        int64
	BottleneckBandwidthBytesPerSecond int64
	PacingRateBytesPerSecond          int64
	InflightTargetBytes               int64
	InflightLimitBytes                int64
	AvailableTokensBytes              int64
	AppLimited                        bool
	LossSampleBytes                   int64
	LossSampleLostBytes               int64
	LossRatio                         float64
	LastFeedbackAt                    time.Time
}

// CongestionState identifies the current congestion controller phase.
type CongestionState uint8

const (
	CongestionStateUnknown CongestionState = iota
	CongestionStateStartup
	CongestionStateDrain
	CongestionStateProbeBandwidth
	CongestionStateProbeRTT
)

type clientStats struct {
	createdAt int64

	outboundTransfersStarted   atomic.Uint64
	outboundTransfersCompleted atomic.Uint64
	outboundTransfersTimedOut  atomic.Uint64
	outboundTransfersFailed    atomic.Uint64
	outboundTransfersCanceled  atomic.Uint64
	outboundSymbolsSent        atomic.Uint64
	outboundSymbolBytesSent    atomic.Uint64
	outboundConfirmations      atomic.Uint64
	outboundConfirmedSymbols   atomic.Uint64
	outboundLastSymbolAt       atomic.Int64
	outboundLastConfirmationAt atomic.Int64

	inboundTransfersStarted      atomic.Uint64
	inboundTransfersCompleted    atomic.Uint64
	inboundTransfersExpired      atomic.Uint64
	inboundTransfersCanceled     atomic.Uint64
	inboundSymbolsReceived       atomic.Uint64
	inboundSymbolBytesReceived   atomic.Uint64
	inboundRepairSymbolsReceived atomic.Uint64
	inboundPayloadBytesDecoded   atomic.Uint64
	inboundProcessingErrors      atomic.Uint64
	inboundConfirmationsSent     atomic.Uint64
	inboundConfirmationErrors    atomic.Uint64
	inboundLastSymbolAt          atomic.Int64
	inboundLastCompleteAt        atomic.Int64
	inboundLastConfirmationAt    atomic.Int64
}

func (r *RLDP) Stats() Stats {
	r.mx.RLock()
	active := ActiveStats{
		Requests:          len(r.activeRequests),
		OutboundTransfers: len(r.activeTransfers),
		InboundStreams:    len(r.recvStreams),
	}
	isClosed := r.closed
	r.mx.RUnlock()
	isClosed = isClosed || r.adnl.GetCloserCtx().Err() != nil

	version := Version1
	if r.useV2.Load() {
		version = Version2
	}

	congestion := r.rateCtrl.stats()
	congestion.LastFeedbackAt = statsTime(r.stats.outboundLastConfirmationAt.Load())

	stats := Stats{
		CreatedAt: statsTime(r.stats.createdAt),
		Closed:    isClosed,
		Version:   version,
		Active:    active,
		Outbound: OutboundStats{
			TransfersStarted:      r.stats.outboundTransfersStarted.Load(),
			TransfersCompleted:    r.stats.outboundTransfersCompleted.Load(),
			TransfersTimedOut:     r.stats.outboundTransfersTimedOut.Load(),
			TransfersFailed:       r.stats.outboundTransfersFailed.Load(),
			TransfersCanceled:     r.stats.outboundTransfersCanceled.Load(),
			SymbolsSent:           r.stats.outboundSymbolsSent.Load(),
			SymbolBytesSent:       r.stats.outboundSymbolBytesSent.Load(),
			ConfirmationsReceived: r.stats.outboundConfirmations.Load(),
			ConfirmedSymbols:      r.stats.outboundConfirmedSymbols.Load(),
			LastSymbolAt:          statsTime(r.stats.outboundLastSymbolAt.Load()),
			LastConfirmationAt:    statsTime(r.stats.outboundLastConfirmationAt.Load()),
		},
		Inbound: InboundStats{
			TransfersStarted:       r.stats.inboundTransfersStarted.Load(),
			TransfersCompleted:     r.stats.inboundTransfersCompleted.Load(),
			TransfersExpired:       r.stats.inboundTransfersExpired.Load(),
			TransfersCanceled:      r.stats.inboundTransfersCanceled.Load(),
			SymbolsReceived:        r.stats.inboundSymbolsReceived.Load(),
			SymbolBytesReceived:    r.stats.inboundSymbolBytesReceived.Load(),
			RepairSymbolsReceived:  r.stats.inboundRepairSymbolsReceived.Load(),
			PayloadBytesDecoded:    r.stats.inboundPayloadBytesDecoded.Load(),
			ProcessingErrors:       r.stats.inboundProcessingErrors.Load(),
			ConfirmationsSent:      r.stats.inboundConfirmationsSent.Load(),
			ConfirmationSendErrors: r.stats.inboundConfirmationErrors.Load(),
			LastSymbolAt:           statsTime(r.stats.inboundLastSymbolAt.Load()),
			LastCompleteAt:         statsTime(r.stats.inboundLastCompleteAt.Load()),
			LastConfirmationAt:     statsTime(r.stats.inboundLastConfirmationAt.Load()),
		},
		Congestion: congestion,
	}
	stats.CapturedAt = time.Now()
	return stats
}

func (c *BBRv2Controller) stats() CongestionStats {
	state := CongestionStateUnknown
	switch c.state.Load() {
	case 0:
		state = CongestionStateStartup
	case 1:
		state = CongestionStateDrain
	case 2:
		state = CongestionStateProbeBandwidth
	case 3:
		state = CongestionStateProbeRTT
	}

	rttObserved := !c.minRTTProvisional.Load()
	var latestRTT time.Duration
	var minRTT time.Duration
	if rttObserved {
		latestRTT = time.Duration(c.lastRTT.Load()) * time.Millisecond
		minRTT = time.Duration(c.minRTT.Load()) * time.Millisecond
	}
	lossSampleBytes, lossSampleLostBytes, lossRatio := c.LastLossSample()

	return CongestionStats{
		State:                             state,
		RTTObserved:                       rttObserved,
		LatestRTT:                         latestRTT,
		MinRTT:                            minRTT,
		DeliveryRateBytesPerSecond:        c.deliveryRate.Load(),
		BottleneckBandwidthBytesPerSecond: c.btlbw.Load(),
		PacingRateBytesPerSecond:          c.pacingRate.Load(),
		InflightTargetBytes:               c.inflight.Load(),
		InflightLimitBytes:                c.hiInflight.Load(),
		AvailableTokensBytes:              c.limiter.GetTokensLeft(),
		AppLimited:                        c.appLimited.Load(),
		LossSampleBytes:                   lossSampleBytes,
		LossSampleLostBytes:               lossSampleLostBytes,
		LossRatio:                         lossRatio,
	}
}

func (s *clientStats) noteOutboundSymbols(count uint64, symbolSize uint32, at time.Time) {
	if count == 0 {
		return
	}

	s.outboundSymbolsSent.Add(count)
	s.outboundSymbolBytesSent.Add(count * uint64(symbolSize))
	storeLatestStatsTime(&s.outboundLastSymbolAt, at)
}

func (s *clientStats) noteOutboundConfirmation(confirmed uint64, at time.Time) {
	s.outboundConfirmations.Add(1)
	s.outboundConfirmedSymbols.Add(confirmed)
	storeLatestStatsTime(&s.outboundLastConfirmationAt, at)
}

func (s *clientStats) noteInboundPart(symbols, bytes, repairSymbols uint64, at time.Time) {
	if symbols == 0 {
		return
	}

	s.inboundSymbolsReceived.Add(symbols)
	s.inboundSymbolBytesReceived.Add(bytes)
	s.inboundRepairSymbolsReceived.Add(repairSymbols)
	storeLatestStatsTime(&s.inboundLastSymbolAt, at)
}

func (s *clientStats) noteInboundComplete(size uint64, at time.Time) {
	s.inboundTransfersCompleted.Add(1)
	s.inboundPayloadBytesDecoded.Add(size)
	storeLatestStatsTime(&s.inboundLastCompleteAt, at)
}

func (s *clientStats) noteInboundConfirmation(err error, at time.Time) {
	if err != nil {
		s.inboundConfirmationErrors.Add(1)
		return
	}

	s.inboundConfirmationsSent.Add(1)
	storeLatestStatsTime(&s.inboundLastConfirmationAt, at)
}

func storeLatestStatsTime(dst *atomic.Int64, value time.Time) {
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

func statsTime(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}

	return time.Unix(0, value)
}
