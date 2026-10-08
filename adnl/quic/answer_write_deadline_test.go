package quic

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"testing"
	"time"
)

// serveAdmittedStream (transport.go) writes a query answer under an idle write
// deadline, so a peer that reads the query but withholds answer-write
// flow-control credit cannot pin its stream admission forever. The deadline is
// idle-based, so it fires here precisely because the write makes no progress at
// all; a slow peer that keeps draining would not be cut off.
// The existing phase1 tests exercise only the read side of admission release;
// this one drives the answer-write side.
//
// The client opens a raw stream, sends a complete valid quic.query, and never
// reads. The handler returns an 8 MiB answer, which exceeds every flow-control
// window the client grants without reading (max 6 MiB per stream, ~4 MiB per
// connection), so the server's answer write is guaranteed to block on the
// client's withheld credit. With the write deadline the blocked write is torn
// down after StreamReadTimeout and the admission slot is released; without it
// the slot would stay pinned for the connection's lifetime.
func TestInboundAnswerWriteDeadlineReleasesAdmission(t *testing.T) {
	const answerSize = 8 << 20

	limits := DefaultLimits()
	limits.MaxConcurrentIncomingStreams = 1
	limits.MaxConcurrentIncomingStreamsPerConnection = 1
	limits.GuaranteedStreamsPerConnection = 1
	limits.StreamReadTimeout = 500 * time.Millisecond

	largeAnswer := bytes.Repeat([]byte{0x5A}, answerSize)
	handler := Handler{
		OnQuery: func(_ context.Context, _ ed25519.PublicKey, _ []byte) ([]byte, error) {
			return largeAnswer, nil
		},
	}
	addr, server := startPhase1ReviewServer(t, handler, limits, mustKey(t))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// A raw stream carrying a complete, valid query whose answer we never read.
	st, err := client.conn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open query stream: %v", err)
	}
	defer st.CancelRead(0)
	if err = writeBoxedObject(st, idQuicQuery, []byte("withhold-answer-credit")); err != nil {
		t.Fatalf("write query: %v", err)
	}

	// The server admits the stream, reads the query, and blocks writing the
	// answer against the client's unadvanced receive window.
	// The lease still holds its byte charge while the answer write is blocked;
	// the stream slot itself is one of this connection's guaranteed ones, so it
	// is not visible in the shared pool.
	waitPhase1ReviewCondition(t, 3*time.Second, "answer write blocked with the admission held", func() bool {
		_, payloadBytes := phase1ReviewAdmissionUsage(server.admission)
		return payloadBytes > 0
	})
	blockedAt := time.Now()

	// The idle write deadline fires and frees the admission.
	waitPhase1ReviewCondition(t, limits.StreamReadTimeout+3*time.Second, "write-deadline admission release", func() bool {
		streams, payloadBytes := phase1ReviewAdmissionUsage(server.admission)
		return streams == 0 && payloadBytes == 0
	})

	// The release must be timed with the write deadline, not an immediate
	// completion: if the write had not actually blocked, the slot would have
	// freed at once and this test would not prove the deadline fired.
	if released := time.Since(blockedAt); released < limits.StreamReadTimeout/2 {
		t.Fatalf(
			"admission released after %v, too fast for the write deadline; the answer write did not block",
			released,
		)
	}
}
