package quic

import (
	"errors"
	"sync/atomic"
)

var (
	errPayloadAdmissionFull    = errors.New("quic: buffered payload byte admission limit reached")
	errStreamAdmissionReleased = errors.New("quic: stream admission already released")
)

// One accounting scope for inbound stream processing: a stream-slot semaphore and
// a buffered-payload byte budget.
//
//   - Slots are a buffered channel, not a mutex plus sync.Cond: channels hand the
//     slot to the longest waiter, cond.Signal to an arbitrary one.
//   - Bytes are charged as they ARRIVE, never reserved from the declared length,
//     matching QuicSender::StreamState::append. A peer that announces 16 MiB and
//     sends one byte costs one chunk.
type streamAdmission struct {
	slots chan struct{}

	maxPayloadBytes int64
	payloadBytes    atomic.Int64
}

func newStreamAdmission(maxStreams int, maxPayloadBytes int64) *streamAdmission {
	return &streamAdmission{
		slots:           make(chan struct{}, maxStreams),
		maxPayloadBytes: maxPayloadBytes,
	}
}

// tryAcquireSlot takes a slot without blocking.
func (a *streamAdmission) tryAcquireSlot() bool {
	select {
	case a.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// acquireSlot blocks until a slot is free or done is closed. Callers take it
// before AcceptStream for per-connection admission, preserving QUIC backpressure.
// Receiver-wide slots are taken after AcceptStream so idle connections do not
// reserve capacity needed by active peers.
func (a *streamAdmission) acquireSlot(done <-chan struct{}) bool {
	select {
	case a.slots <- struct{}{}:
		return true
	case <-done:
		return false
	}
}

func (a *streamAdmission) releaseSlot() {
	select {
	case <-a.slots:
	default:
	}
}

func (a *streamAdmission) activeStreams() int {
	return len(a.slots)
}

func (a *streamAdmission) tryChargeBytes(n int64) bool {
	if a.payloadBytes.Add(n) > a.maxPayloadBytes {
		a.payloadBytes.Add(-n)
		return false
	}
	return true
}

func (a *streamAdmission) releaseBytes(n int64) {
	if n != 0 {
		a.payloadBytes.Add(-n)
	}
}

func (a *streamAdmission) reservedPayloadBytes() int64 {
	return a.payloadBytes.Load()
}

// Owns the byte charges of one inbound stream, plus the global slot when one was
// taken; the per-connection slot stays with the accept loop. The zero lease owns
// nothing - what an outbound-dialed connection's inbound streams get.
type streamAdmissionLease struct {
	admission  *streamAdmission
	connection *streamAdmission

	globalSlot bool
	charged    int64
	// Atomic so a lease shared between the accept loop's error path and the stream
	// goroutine is released from either exactly once. chargeBytes needs no atomic:
	// it is only called by the reading goroutine.
	released atomic.Bool
}

// Called once per read chunk, so a stalled peer stops costing memory the moment
// it stops sending.
func (l *streamAdmissionLease) chargeBytes(n int64) error {
	if l.released.Load() {
		return errStreamAdmissionReleased
	}
	if n <= 0 {
		return nil
	}
	if l.admission != nil && !l.admission.tryChargeBytes(n) {
		return errPayloadAdmissionFull
	}
	if l.connection != nil && !l.connection.tryChargeBytes(n) {
		if l.admission != nil {
			l.admission.releaseBytes(n)
		}
		return errPayloadAdmissionFull
	}
	l.charged += n
	return nil
}

// release returns everything the lease owns. Only the first call takes effect.
func (l *streamAdmissionLease) release() {
	if !l.released.CompareAndSwap(false, true) {
		return
	}

	if l.admission != nil {
		l.admission.releaseBytes(l.charged)
		if l.globalSlot {
			l.admission.releaseSlot()
		}
	}
	if l.connection != nil {
		l.connection.releaseBytes(l.charged)
	}
}
