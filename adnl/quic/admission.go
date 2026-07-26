package quic

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

var (
	errPayloadAdmissionFull    = errors.New("quic: reserved payload byte admission limit reached")
	errPayloadAlreadyReserved  = errors.New("quic: payload bytes already reserved")
	errStreamAdmissionReleased = errors.New("quic: stream admission already released")
)

// streamAdmission bounds concurrent stream processing and payload memory
// reserved by those streams. Stream slots are blocking: an exhausted scope
// backpressures the acceptor until a lease is released. Payload byte
// reservations stay nonblocking.
type streamAdmission struct {
	mu   sync.Mutex
	cond sync.Cond

	maxStreams    uint64
	activeStreams uint64

	maxPayloadBytes      uint64
	reservedPayloadBytes atomic.Uint64
}

func newStreamAdmission(maxStreams, maxPayloadBytes uint64) *streamAdmission {
	a := &streamAdmission{
		maxStreams:      maxStreams,
		maxPayloadBytes: maxPayloadBytes,
	}
	a.cond.L = &a.mu
	return a
}

// acquireStream blocks until a receiver-wide stream slot is free. The caller
// must release the returned lease on every exit path.
func (a *streamAdmission) acquireStream(ctx context.Context) (streamAdmissionLease, error) {
	return a.acquireStreamWithin(ctx, nil)
}

// acquireStreamWithin blocks until both the receiver-wide admission and a
// connection-local admission have a free stream slot. Keeping both counters in
// one lease makes release atomic from the stream owner's point of view.
func (a *streamAdmission) acquireStreamWithin(ctx context.Context, connection *streamAdmission) (streamAdmissionLease, error) {
	if connection != nil {
		if err := connection.acquireSlot(ctx); err != nil {
			return streamAdmissionLease{}, err
		}
	}
	if err := a.acquireSlot(ctx); err != nil {
		if connection != nil {
			connection.releaseSlot()
		}
		return streamAdmissionLease{}, err
	}
	return streamAdmissionLease{admission: a, connection: connection}, nil
}

// acquireSlot blocks until a stream slot is free. Waiters observe ctx
// cancellation only when woken; the caller must arrange a wake call on ctx
// cancellation before blocking here.
func (a *streamAdmission) acquireSlot(ctx context.Context) error {
	a.mu.Lock()
	for a.activeStreams >= a.maxStreams {
		if err := ctx.Err(); err != nil {
			a.mu.Unlock()
			return err
		}
		a.cond.Wait()
	}
	a.activeStreams++
	a.mu.Unlock()
	return nil
}

func (a *streamAdmission) releaseSlot() {
	a.mu.Lock()
	a.activeStreams--
	a.cond.Signal()
	a.mu.Unlock()
}

// wake lets slot waiters observe context cancellation. Broadcasting under the
// mutex pairs with the waiters' locked pre-Wait ctx check, so a concurrent
// waiter either sees the cancelled ctx or receives this broadcast.
func (a *streamAdmission) wake() {
	a.mu.Lock()
	a.cond.Broadcast()
	a.mu.Unlock()
}

func (a *streamAdmission) tryReservePayloadBytes(payloadBytes uint64) bool {
	if a.reservedPayloadBytes.Add(payloadBytes) > a.maxPayloadBytes {
		a.reservedPayloadBytes.Add(-payloadBytes)
		return false
	}
	return true
}

func (a *streamAdmission) releasePayloadBytes(payloadBytes uint64) {
	a.reservedPayloadBytes.Add(-payloadBytes)
}

// streamAdmissionLease owns one stream slot per scope and, after
// reservePayload succeeds, the payload bytes assigned to that stream. The
// zero lease owns nothing.
type streamAdmissionLease struct {
	admission  *streamAdmission
	connection *streamAdmission

	payloadBytes    uint64
	payloadReserved bool
	released        bool
}

// reservePayload reserves memory after the payload length has been parsed and
// before the payload buffer is allocated. A lease may reserve payload once.
func (l *streamAdmissionLease) reservePayload(payloadBytes uint64) error {
	if l.released {
		return errStreamAdmissionReleased
	}
	if l.payloadReserved {
		return errPayloadAlreadyReserved
	}
	if !l.admission.tryReservePayloadBytes(payloadBytes) {
		return errPayloadAdmissionFull
	}
	if l.connection != nil && !l.connection.tryReservePayloadBytes(payloadBytes) {
		l.admission.releasePayloadBytes(payloadBytes)
		return errPayloadAdmissionFull
	}
	l.payloadBytes = payloadBytes
	l.payloadReserved = true
	return nil
}

// release returns all resources owned by the lease and wakes blocked
// acquirers. It is safe to call concurrently and more than once on the same
// lease; only the first call takes effect.
func (l *streamAdmissionLease) release() {
	a := l.admission
	if a == nil {
		return
	}

	a.mu.Lock()
	if l.released {
		a.mu.Unlock()
		return
	}
	l.released = true
	a.activeStreams--
	a.cond.Signal()
	a.mu.Unlock()

	if l.connection != nil {
		l.connection.releaseSlot()
	}
	if l.payloadReserved {
		a.releasePayloadBytes(l.payloadBytes)
		if l.connection != nil {
			l.connection.releasePayloadBytes(l.payloadBytes)
		}
	}
}
