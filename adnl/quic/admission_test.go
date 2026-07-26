package quic

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func expiredContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestStreamAdmissionAcquire(t *testing.T) {
	admission := newStreamAdmission(2, 100)
	ctx := context.Background()

	first, err := admission.acquireStream(ctx)
	if err != nil {
		t.Fatalf("acquire first stream: %v", err)
	}
	second, err := admission.acquireStream(ctx)
	if err != nil {
		t.Fatalf("acquire second stream: %v", err)
	}
	if admission.activeStreams != 2 {
		t.Fatalf("active streams = %d, want 2", admission.activeStreams)
	}
	if got := admission.reservedPayloadBytes.Load(); got != 0 {
		t.Fatalf("reserved payload bytes before reserve = %d, want 0", got)
	}

	if _, err = admission.acquireStream(expiredContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire over limit error = %v, want %v", err, context.Canceled)
	}

	first.release()
	first.release()

	third, err := admission.acquireStream(ctx)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if _, err = admission.acquireStream(expiredContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire after duplicate release error = %v, want %v", err, context.Canceled)
	}

	second.release()
	third.release()
	if admission.activeStreams != 0 {
		t.Fatalf("active streams after release = %d, want 0", admission.activeStreams)
	}
}

func TestStreamAdmissionReleaseWakesBlockedAcquire(t *testing.T) {
	admission := newStreamAdmission(1, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lease, err := admission.acquireStream(ctx)
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan error, 1)
	go func() {
		next, acquireErr := admission.acquireStream(ctx)
		if acquireErr == nil {
			next.release()
		}
		acquired <- acquireErr
	}()

	select {
	case err = <-acquired:
		t.Fatalf("second acquire returned %v before the slot was released", err)
	case <-time.After(50 * time.Millisecond):
	}

	lease.release()
	select {
	case err = <-acquired:
		if err != nil {
			t.Fatalf("second acquire after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second acquire was not woken by release")
	}
	if admission.activeStreams != 0 {
		t.Fatalf("active streams = %d, want 0", admission.activeStreams)
	}
}

func TestStreamAdmissionWakeUnblocksCancelledAcquire(t *testing.T) {
	admission := newStreamAdmission(1, 0)
	lease, err := admission.acquireStream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopWake := context.AfterFunc(ctx, admission.wake)
	defer stopWake()

	acquired := make(chan error, 1)
	go func() {
		_, acquireErr := admission.acquireStream(ctx)
		acquired <- acquireErr
	}()

	select {
	case err = <-acquired:
		t.Fatalf("blocked acquire returned %v before cancellation", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case err = <-acquired:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled acquire error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked acquire was not woken by ctx cancellation")
	}
}

func TestStreamAdmissionReservePayload(t *testing.T) {
	admission := newStreamAdmission(3, 100)
	ctx := context.Background()

	first, err := admission.acquireStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := admission.acquireStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	third, err := admission.acquireStream(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err = first.reservePayload(60); err != nil {
		t.Fatalf("reserve first payload: %v", err)
	}
	if err = second.reservePayload(40); err != nil {
		t.Fatalf("reserve second payload: %v", err)
	}
	if err = third.reservePayload(1); !errors.Is(err, errPayloadAdmissionFull) {
		t.Fatalf("reserve over limit error = %v, want %v", err, errPayloadAdmissionFull)
	}
	if got := admission.reservedPayloadBytes.Load(); got != 100 {
		t.Fatalf("reserved payload bytes = %d, want 100", got)
	}

	first.release()
	first.release()
	if got := admission.reservedPayloadBytes.Load(); got != 40 {
		t.Fatalf("reserved payload bytes after release = %d, want 40", got)
	}
	if err = third.reservePayload(60); err != nil {
		t.Fatalf("reserve after capacity release: %v", err)
	}

	second.release()
	third.release()
	if got := admission.reservedPayloadBytes.Load(); got != 0 {
		t.Fatalf("reserved payload bytes after all releases = %d, want 0", got)
	}
}

func TestStreamAdmissionReservationLifecycle(t *testing.T) {
	t.Run("zero payload", func(t *testing.T) {
		admission := newStreamAdmission(1, 0)
		lease, err := admission.acquireStream(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if err = lease.reservePayload(0); err != nil {
			t.Fatalf("reserve empty payload: %v", err)
		}
		if err = lease.reservePayload(0); !errors.Is(err, errPayloadAlreadyReserved) {
			t.Fatalf("second reserve error = %v, want %v", err, errPayloadAlreadyReserved)
		}

		lease.release()
	})

	t.Run("payload limit zero", func(t *testing.T) {
		admission := newStreamAdmission(1, 0)
		lease, err := admission.acquireStream(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer lease.release()

		if err = lease.reservePayload(1); !errors.Is(err, errPayloadAdmissionFull) {
			t.Fatalf("reserve error = %v, want %v", err, errPayloadAdmissionFull)
		}
	})

	t.Run("reserve after release", func(t *testing.T) {
		admission := newStreamAdmission(1, 1)
		lease, err := admission.acquireStream(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		lease.release()

		if err = lease.reservePayload(1); !errors.Is(err, errStreamAdmissionReleased) {
			t.Fatalf("reserve after release error = %v, want %v", err, errStreamAdmissionReleased)
		}
	})
}

func TestStreamAdmissionConcurrentRelease(t *testing.T) {
	admission := newStreamAdmission(1, 64)
	lease, err := admission.acquireStream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.reservePayload(64); err != nil {
		t.Fatal(err)
	}

	var releases sync.WaitGroup
	for range 128 {
		releases.Add(1)
		go func() {
			defer releases.Done()
			lease.release()
		}()
	}
	releases.Wait()

	if admission.activeStreams != 0 {
		t.Fatalf("active streams = %d, want 0", admission.activeStreams)
	}
	if got := admission.reservedPayloadBytes.Load(); got != 0 {
		t.Fatalf("reserved payload bytes = %d, want 0", got)
	}

	next, err := admission.acquireStream(context.Background())
	if err != nil {
		t.Fatalf("acquire after concurrent release: %v", err)
	}
	if err = next.reservePayload(64); err != nil {
		t.Fatalf("reserve after concurrent release: %v", err)
	}
	next.release()
}

func TestStreamAdmissionConcurrentLimits(t *testing.T) {
	const (
		workers          = 64
		maxStreams       = 8
		maxPayloadBytes  = 32
		payloadBytes     = 8
		expectedAdmitted = maxPayloadBytes / payloadBytes
	)

	admission := newStreamAdmission(maxStreams, maxPayloadBytes)
	start := make(chan struct{})
	release := make(chan struct{})

	var attempted sync.WaitGroup
	attempted.Add(workers)
	var finished sync.WaitGroup
	finished.Add(workers)

	var admitted atomic.Int64
	var active atomic.Int64
	var peak atomic.Int64

	for range workers {
		go func() {
			defer finished.Done()
			<-start

			lease, err := admission.acquireStream(context.Background())
			if err != nil {
				attempted.Done()
				return
			}
			if err = lease.reservePayload(payloadBytes); err != nil {
				lease.release()
				attempted.Done()
				return
			}

			admitted.Add(1)
			current := active.Add(1)
			for {
				previous := peak.Load()
				if current <= previous || peak.CompareAndSwap(previous, current) {
					break
				}
			}
			attempted.Done()

			runtime.Gosched()
			<-release
			active.Add(-1)
			lease.release()
		}()
	}

	close(start)
	attempted.Wait()

	if got := admitted.Load(); got != expectedAdmitted {
		t.Fatalf("admitted streams = %d, want %d", got, expectedAdmitted)
	}
	if got := peak.Load(); got > expectedAdmitted {
		t.Fatalf("peak admitted streams = %d, want at most %d", got, expectedAdmitted)
	}

	close(release)
	finished.Wait()

	if admission.activeStreams != 0 {
		t.Fatalf("active streams after stress = %d, want 0", admission.activeStreams)
	}
	if got := admission.reservedPayloadBytes.Load(); got != 0 {
		t.Fatalf("reserved payload bytes after stress = %d, want 0", got)
	}
}
