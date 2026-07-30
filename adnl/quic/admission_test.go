package quic

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestStreamAdmissionSlots(t *testing.T) {
	admission := newStreamAdmission(2, 1024)

	if !admission.tryAcquireSlot() || !admission.tryAcquireSlot() {
		t.Fatal("slots within the limit were refused")
	}
	if admission.tryAcquireSlot() {
		t.Fatal("slot beyond the limit was granted")
	}
	if got := admission.activeStreams(); got != 2 {
		t.Fatalf("activeStreams = %d, want 2", got)
	}

	admission.releaseSlot()
	if !admission.tryAcquireSlot() {
		t.Fatal("slot freed by release was not reusable")
	}
}

// The slot semaphore is a channel so a blocked acceptor is released in arrival
// order; sync.Cond.Signal would wake an arbitrary one and let a busy peer keep
// winning the slot.
func TestStreamAdmissionAcquireSlotBlocksUntilRelease(t *testing.T) {
	admission := newStreamAdmission(1, 1024)
	if !admission.tryAcquireSlot() {
		t.Fatal("first slot was refused")
	}

	done := make(chan struct{})
	acquired := make(chan bool, 1)
	go func() { acquired <- admission.acquireSlot(done) }()

	select {
	case <-acquired:
		t.Fatal("acquireSlot returned while the limit was exhausted")
	case <-time.After(50 * time.Millisecond):
	}

	admission.releaseSlot()
	select {
	case ok := <-acquired:
		if !ok {
			t.Fatal("acquireSlot reported failure after a slot was freed")
		}
	case <-time.After(time.Second):
		t.Fatal("acquireSlot did not wake after release")
	}
}

func TestStreamAdmissionAcquireSlotUnblocksOnDone(t *testing.T) {
	admission := newStreamAdmission(1, 1024)
	if !admission.tryAcquireSlot() {
		t.Fatal("first slot was refused")
	}

	done := make(chan struct{})
	acquired := make(chan bool, 1)
	go func() { acquired <- admission.acquireSlot(done) }()

	close(done)
	select {
	case ok := <-acquired:
		if ok {
			t.Fatal("acquireSlot granted a slot after done was closed")
		}
	case <-time.After(time.Second):
		t.Fatal("acquireSlot did not observe done")
	}
}

// Bytes are charged as they arrive, so a lease grows its charge across chunks
// and gives all of it back at once.
func TestStreamAdmissionChargeBytesAccumulates(t *testing.T) {
	global := newStreamAdmission(4, 300)
	connection := newStreamAdmission(4, 200)
	lease := streamAdmissionLease{admission: global, connection: connection}

	for i := 0; i < 2; i++ {
		if err := lease.chargeBytes(100); err != nil {
			t.Fatalf("charge %d rejected: %v", i, err)
		}
	}
	if got := global.reservedPayloadBytes(); got != 200 {
		t.Fatalf("global charged = %d, want 200", got)
	}
	if got := connection.reservedPayloadBytes(); got != 200 {
		t.Fatalf("connection charged = %d, want 200", got)
	}

	// The per-connection budget is the tighter one and must reject first,
	// without leaving a partial charge behind on the global scope.
	if err := lease.chargeBytes(100); !errors.Is(err, errPayloadAdmissionFull) {
		t.Fatalf("charge past the per-connection budget = %v, want %v", err, errPayloadAdmissionFull)
	}
	if got := global.reservedPayloadBytes(); got != 200 {
		t.Fatalf("global charged after a rejected chunk = %d, want 200 (no leak)", got)
	}

	lease.release()
	if got := global.reservedPayloadBytes(); got != 0 {
		t.Fatalf("global charged after release = %d, want 0", got)
	}
	if got := connection.reservedPayloadBytes(); got != 0 {
		t.Fatalf("connection charged after release = %d, want 0", got)
	}
}

func TestStreamAdmissionReleaseIsIdempotent(t *testing.T) {
	global := newStreamAdmission(2, 1024)
	if !global.tryAcquireSlot() {
		t.Fatal("slot was refused")
	}
	lease := streamAdmissionLease{admission: global, globalSlot: true}
	if err := lease.chargeBytes(64); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			lease.release()
		}()
	}
	close(start)
	wg.Wait()

	if got := global.reservedPayloadBytes(); got != 0 {
		t.Fatalf("charged after concurrent release = %d, want 0", got)
	}
	if got := global.activeStreams(); got != 0 {
		t.Fatalf("activeStreams after concurrent release = %d, want 0", got)
	}
	if err := lease.chargeBytes(1); !errors.Is(err, errStreamAdmissionReleased) {
		t.Fatalf("charge after release = %v, want %v", err, errStreamAdmissionReleased)
	}
}

// A peer that declares a huge payload and sends nothing must cost one chunk,
// not the declared length. This is the property that keeps a few silent peers
// from holding the whole byte budget and dropping everyone else's broadcasts.
func TestReadAdmittedPayloadChargesOnlyArrivedBytes(t *testing.T) {
	global := newStreamAdmission(4, 64<<20)
	lease := streamAdmissionLease{admission: global}

	declared := 16 << 20
	if _, err := readAdmittedPayload(bytes.NewReader(make([]byte, 4)), declared, &lease); err == nil {
		t.Fatal("a truncated payload was accepted")
	}
	if got := global.reservedPayloadBytes(); got > payloadReadChunk {
		t.Fatalf("charged %d bytes for a peer that sent 4, want at most one chunk (%d)", got, payloadReadChunk)
	}

	lease.release()
	if got := global.reservedPayloadBytes(); got != 0 {
		t.Fatalf("charged after release = %d, want 0", got)
	}
}

func TestReadAdmittedPayloadReadsWholePayload(t *testing.T) {
	for _, size := range []int{1, payloadReadChunk - 1, payloadReadChunk, payloadReadChunk + 1, payloadCommitThreshold + 7} {
		want := make([]byte, size)
		for i := range want {
			want[i] = byte(i)
		}

		global := newStreamAdmission(4, 64<<20)
		lease := streamAdmissionLease{admission: global}
		got, err := readAdmittedPayload(bytes.NewReader(want), size, &lease)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("size %d: payload mismatch", size)
		}
		if charged := global.reservedPayloadBytes(); charged != int64(size) {
			t.Fatalf("size %d: charged %d, want %d", size, charged, size)
		}
	}
}

// A stream the admission refuses must not allocate a payload buffer at all,
// otherwise the refusal itself becomes the amplification primitive.
func TestReadAdmittedPayloadRefusalDoesNotAllocate(t *testing.T) {
	admission := newStreamAdmission(1, 1)
	data := make([]byte, 4)

	allocs := testing.AllocsPerRun(100, func() {
		lease := streamAdmissionLease{admission: admission}
		payload, err := readAdmittedPayload(bytes.NewReader(data), 16<<20, &lease)
		if err == nil {
			t.Fatal("a payload over the byte budget was accepted")
		}
		if payload != nil {
			t.Fatalf("refused payload allocated %d bytes", len(payload))
		}
		lease.release()
	})
	// Only the bytes.Reader itself; no payload buffer.
	if allocs > 2 {
		t.Fatalf("refused stream allocated %v times, want at most 2", allocs)
	}
}
