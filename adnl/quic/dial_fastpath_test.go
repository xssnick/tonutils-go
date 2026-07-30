package quic

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Once a peer has a live outbound connection there is nothing to dial, so
// reaching it must not take dialMu - taking it made every send to a peer queue
// behind every other send to it. The test holds dialMu for the whole call, so a
// regression blocks until the deadline instead of returning the path.
func TestGatewayEstablishedDialSkipsDialLock(t *testing.T) {
	server, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	server.SetConnectionHandler(func(peer *Peer) error { return nil })
	addr := startGateway(t, server)

	client, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	peer, err := client.DialDefault(ctx, server.PublicKey(), addr)
	if err != nil {
		t.Fatalf("establish path: %v", err)
	}

	peer.dialMu.Lock()
	defer peer.dialMu.Unlock()

	reused := make(chan error, 1)
	go func() {
		_, dialErr := client.DialDefault(ctx, server.PublicKey(), addr)
		reused <- dialErr
	}()

	select {
	case dialErr := <-reused:
		if dialErr != nil {
			t.Fatalf("reuse established path: %v", dialErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reusing an established path blocked on the dial lock")
	}
}

// The shape the node produces: many goroutines reaching the same already-connected
// peer at once must all get the same path without serialising on it.
func TestGatewayConcurrentSendsShareEstablishedPath(t *testing.T) {
	server, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	server.SetConnectionHandler(func(peer *Peer) error { return nil })
	addr := startGateway(t, server)

	client, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	established, err := client.DialDefault(ctx, server.PublicKey(), addr)
	if err != nil {
		t.Fatalf("establish path: %v", err)
	}

	const senders = 32
	var wg sync.WaitGroup
	peers := make([]*Peer, senders)
	errs := make([]error, senders)
	for i := range senders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			peers[i], errs[i] = client.DialDefault(ctx, server.PublicKey(), addr)
		}()
	}
	wg.Wait()

	for i := range senders {
		if errs[i] != nil {
			t.Fatalf("concurrent reuse %d: %v", i, errs[i])
		}
		if peers[i] != established {
			t.Fatalf("concurrent reuse %d returned a different path", i)
		}
	}
}
