package quic

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// Inbound streams arriving on a connection WE dialed must not be accounted
// against, or gated by, the receiver-wide inbound pool: otherwise a flood of
// connections opened to us degrades the queries and broadcasts flowing over
// peers we chose to talk to. The reference node draws the same line at
// quic-server.cpp:653 for connection flood control.
func TestOutboundDialedConnectionSkipsInboundAdmission(t *testing.T) {
	serverKey := mustKey(t)
	clientKey := mustKey(t)

	server, err := NewGateway(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	server.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			// Turn around and query back over the same connection, which the
			// client accepted as an inbound stream on a path it dialed.
			return peer.Query(ctx, []byte("reverse"), 0)
		})
		return nil
	})
	addr := startGateway(t, server)

	// A pool so small that any use by the dialed connection would show up.
	limits := DefaultLimits()
	limits.MaxConcurrentIncomingStreams = 1
	limits.MaxConcurrentIncomingStreamsPerConnection = 1
	limits.GuaranteedStreamsPerConnection = 1

	client, err := NewGatewayWithLimits(limits, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	reverseSeen := make(chan struct{}, 1)
	client.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			// While serving this inbound stream the dialed connection must not
			// hold anything in the client's inbound pool.
			streams, payloadBytes := phase1ReviewAdmissionUsage(client.admission)
			if streams != 0 || payloadBytes != 0 {
				t.Errorf("dialed connection consumed inbound admission: %d streams, %d bytes",
					streams, payloadBytes)
			}
			select {
			case reverseSeen <- struct{}{}:
			default:
			}
			return append([]byte("client:"), payload...), nil
		})
		return nil
	})
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	peer, err := client.DialDefault(ctx, server.PublicKey(), addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	ans, err := peer.Query(ctx, []byte("start"), 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !bytes.Equal(ans, []byte("client:reverse")) {
		t.Fatalf("answer = %q, want client:reverse", ans)
	}

	select {
	case <-reverseSeen:
	case <-time.After(time.Second):
		t.Fatal("reverse query never reached the dialing gateway")
	}

	if streams, payloadBytes := phase1ReviewAdmissionUsage(client.admission); streams != 0 || payloadBytes != 0 {
		t.Fatalf("inbound admission after a dialed-connection stream = %d streams, %d bytes, want 0/0",
			streams, payloadBytes)
	}
}
