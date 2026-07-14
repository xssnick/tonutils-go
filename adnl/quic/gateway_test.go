package quic

import (
	"bytes"
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func startGateway(t *testing.T, g *Gateway) string {
	t.Helper()

	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}

	go func() { _ = g.Serve(pc) }()
	t.Cleanup(func() { _ = g.Close() })
	return pc.LocalAddr().String()
}

func TestGatewayQueryReusesPath(t *testing.T) {
	serverKey := mustKey(t)
	clientKey := mustKey(t)

	server, err := NewGateway(serverKey)
	if err != nil {
		t.Fatal(err)
	}

	var connected atomic.Int32
	server.SetConnectionHandler(func(peer *Peer) error {
		connected.Add(1)
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			return append([]byte("echo:"), payload...), nil
		})
		return nil
	})
	addr := startGateway(t, server)

	client, err := NewGateway(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	peer, err := client.DialDefault(ctx, server.PublicKey(), addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	for _, req := range [][]byte{[]byte("one"), []byte("two")} {
		ans, err := peer.Query(ctx, req, 0)
		if err != nil {
			t.Fatalf("query %q: %v", req, err)
		}
		if want := append([]byte("echo:"), req...); !bytes.Equal(ans, want) {
			t.Fatalf("answer = %q, want %q", ans, want)
		}
	}

	if connected.Load() != 1 {
		t.Fatalf("connection handler calls = %d, want 1", connected.Load())
	}
	if server.Peer(server.PublicKey(), client.PublicKey()) == nil {
		t.Fatal("server did not register managed peer path")
	}
}

func TestGatewayMultiIdentityPathIsolation(t *testing.T) {
	serverKey1 := mustKey(t)
	serverKey2 := mustKey(t)
	clientKey := mustKey(t)

	server, err := NewGateway(serverKey1, serverKey2)
	if err != nil {
		t.Fatal(err)
	}
	serverKeys := server.Identities()
	if len(serverKeys) != 2 {
		t.Fatalf("identities = %d, want 2", len(serverKeys))
	}

	var mu sync.Mutex
	seen := map[string]int{}
	server.SetConnectionHandler(func(peer *Peer) error {
		mu.Lock()
		seen[string(peer.LocalID())]++
		mu.Unlock()

		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			local := peer.LocalID()
			res := make([]byte, len(local))
			copy(res, local)
			return res, nil
		})
		return nil
	})
	addr := startGateway(t, server)

	client, err := NewGateway(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, key := range serverKeys {
		id, err := idFromPublicKey(key)
		if err != nil {
			t.Fatal(err)
		}
		peer, err := client.Dial(ctx, client.PublicKey(), key, addr)
		if err != nil {
			t.Fatalf("dial %s: %v", id, err)
		}
		ans, err := peer.Query(ctx, []byte("id"), 0)
		if err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		if !bytes.Equal(ans, id[:]) {
			t.Fatalf("answer local id = %x, want %s", ans, id)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for _, key := range serverKeys {
		id, err := idFromPublicKey(key)
		if err != nil {
			t.Fatal(err)
		}
		if got := seen[string(id[:])]; got != 1 {
			t.Fatalf("path %s handler calls = %d, want 1", id, got)
		}
	}
}

func TestGatewayReverseQueryUsesExistingConnection(t *testing.T) {
	serverKey := mustKey(t)
	clientKey := mustKey(t)

	server, err := NewGateway(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	server.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			return peer.Query(ctx, []byte("reverse"), 0)
		})
		return nil
	})
	addr := startGateway(t, server)

	client, err := NewGateway(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	client.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
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
}
