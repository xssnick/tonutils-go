package quic

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"testing"
	"time"
)

// A plumtree broadcast can be up to Overlays::max_fec_broadcast_size (16 MiB)
// and arrives over whatever link the peer has. The read bound must be idle
// based: with a single absolute budget, StreamReadTimeout would impose a
// minimum sustained bandwidth (16 MiB / 15 s ~= 1.1 MB/s) and a slower honest
// peer could never deliver a broadcast, on any retry.
func TestSlowLargeMessageIsNotRejectedByIdleTimeout(t *testing.T) {
	const payloadSize = 2 << 20
	const chunk = 64 << 10

	limits := DefaultLimits()
	// Deliberately far shorter than the whole transfer takes.
	limits.StreamReadTimeout = 300 * time.Millisecond

	delivered := make(chan []byte, 1)
	handler := Handler{
		OnQuery: func(context.Context, ed25519.PublicKey, []byte) ([]byte, error) {
			return nil, nil
		},
		OnMessage: func(_ context.Context, _ ed25519.PublicKey, payload []byte) {
			delivered <- append([]byte(nil), payload...)
		},
	}
	addr, server := startPhase1ReviewServer(t, handler, limits, mustKey(t))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i)
	}

	st, err := client.conn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer st.CancelRead(0)

	header, headerLen, pad, _, err := boxedObjectHeader(idQuicMessage, len(payload))
	if err != nil {
		t.Fatal(err)
	}
	wire := append([]byte(nil), header[:headerLen]...)
	wire = append(wire, payload...)
	wire = append(wire, make([]byte, pad)...)

	// Trickle it out: every pause is shorter than the idle bound, but the whole
	// transfer takes many times longer than it.
	for off := 0; off < len(wire); off += chunk {
		end := off + chunk
		if end > len(wire) {
			end = len(wire)
		}
		if err = writeFull(st, wire[off:end]); err != nil {
			t.Fatalf("write chunk at %d: %v", off, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err = st.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}

	select {
	case got := <-delivered:
		if !bytes.Equal(got, payload) {
			t.Fatalf("delivered %d bytes, want %d", len(got), len(payload))
		}
	case <-time.After(20 * time.Second):
		t.Fatal("a slow large message was dropped; the read bound is not idle based")
	}
}

// The whole point of charging on arrival: peers that declare huge objects and
// go quiet must not be able to hold the budget and make everyone else's
// broadcasts fail.
func TestSilentPeersDoNotBlockRealTraffic(t *testing.T) {
	limits := DefaultLimits()
	// One max-size object worth of global budget, so under reserve-on-declared
	// semantics a single silent peer would consume all of it.
	limits.MaxBufferedIncomingBytes = limits.MaxObjectSize
	limits.MaxBufferedIncomingBytesPerConnection = limits.MaxObjectSize
	limits.StreamReadTimeout = 10 * time.Second

	handler := Handler{
		OnQuery: func(_ context.Context, _ ed25519.PublicKey, payload []byte) ([]byte, error) {
			return payload, nil
		},
	}
	addr, server := startPhase1ReviewServer(t, handler, limits, mustKey(t))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	greedy, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial greedy client: %v", err)
	}
	defer greedy.Close()

	for i := 0; i < 4; i++ {
		st := openPhase1ReviewPartialQuery(t, ctx, greedy, int(limits.MaxObjectSize)-4096)
		defer st.CancelRead(0)
		defer st.CancelWrite(0)
	}

	waitPhase1ReviewCondition(t, 2*time.Second, "greedy streams admitted", func() bool {
		_, payloadBytes := phase1ReviewAdmissionUsage(server.admission)
		return payloadBytes > 0
	})
	if _, payloadBytes := phase1ReviewAdmissionUsage(server.admission); payloadBytes > 4*payloadReadChunk {
		t.Fatalf("four silent streams hold %d bytes of budget, want at most %d",
			payloadBytes, 4*payloadReadChunk)
	}

	honest, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial honest client: %v", err)
	}
	defer honest.Close()

	answer, err := honest.Query(ctx, []byte("still-served"), 0)
	if err != nil {
		t.Fatalf("honest query while silent peers hold streams: %v", err)
	}
	if !bytes.Equal(answer, []byte("still-served")) {
		t.Fatalf("answer = %q, want still-served", answer)
	}
}
