package quic

import (
	"bytes"
	"context"
	"errors"
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

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- g.Serve(pc)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = g.WaitReady(ctx); err != nil {
		_ = g.Close()
		_ = pc.Close()
		t.Fatalf("wait for QUIC gateway: %v", err)
	}

	t.Cleanup(func() {
		_ = g.Close()
		_ = pc.Close()
		if err := <-serveErr; err != nil {
			t.Errorf("serve QUIC gateway: %v", err)
		}
	})
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

func TestGatewayDialReusesOutboundForIdentityPath(t *testing.T) {
	serverKey := mustKey(t)

	firstServer, err := NewGateway(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	firstServer.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(context.Context, []byte) ([]byte, error) {
			return []byte("first"), nil
		})
		return nil
	})
	firstAddr := startGateway(t, firstServer)

	secondServer, err := NewGateway(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	secondServer.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(context.Context, []byte) ([]byte, error) {
			return []byte("second"), nil
		})
		return nil
	})
	secondAddr := startGateway(t, secondServer)

	client, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	peer, err := client.DialDefault(ctx, firstServer.PublicKey(), firstAddr)
	if err != nil {
		t.Fatalf("dial first endpoint: %v", err)
	}
	peer.mu.RLock()
	firstOutbound := peer.outbound
	peer.mu.RUnlock()

	reused, err := client.DialDefault(ctx, firstServer.PublicKey(), firstAddr)
	if err != nil {
		t.Fatalf("redial first endpoint: %v", err)
	}
	reused.mu.RLock()
	reusedOutbound := reused.outbound
	reused.mu.RUnlock()
	if reused != peer || reusedOutbound != firstOutbound {
		t.Fatal("same endpoint did not reuse its outbound connection")
	}

	reused, err = client.DialDefault(ctx, secondServer.PublicKey(), secondAddr)
	if err != nil {
		t.Fatalf("redial identity path with another endpoint: %v", err)
	}
	reused.mu.RLock()
	reusedOutbound = reused.outbound
	reused.mu.RUnlock()
	if reused != peer || reusedOutbound != firstOutbound {
		t.Fatal("same identity path did not reuse its existing outbound connection")
	}

	reused, err = client.DialDefault(ctx, firstServer.PublicKey(), "address-is-not-needed-for-reuse")
	if err != nil {
		t.Fatalf("redial identity path without a usable endpoint: %v", err)
	}
	if reused != peer {
		t.Fatal("outbound reuse depended on the creation endpoint")
	}

	answer, err := reused.Query(ctx, []byte("endpoint"), 0)
	if err != nil {
		t.Fatalf("query reused endpoint: %v", err)
	}
	if !bytes.Equal(answer, []byte("first")) {
		t.Fatalf("answer = %q, want first", answer)
	}
}

func TestGatewayOutboundPeerExcludesInboundOnlyPath(t *testing.T) {
	source, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	inboundPath := make(chan *Peer, 1)
	source.SetConnectionHandler(func(peer *Peer) error {
		inboundPath <- peer
		return nil
	})
	sourceAddr := startGateway(t, source)

	remote, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	remoteAddr := startGateway(t, remote)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err = remote.DialDefault(ctx, source.PublicKey(), sourceAddr); err != nil {
		t.Fatalf("create source inbound path: %v", err)
	}

	var inbound *Peer
	select {
	case inbound = <-inboundPath:
	case <-ctx.Done():
		t.Fatalf("wait for source inbound path: %v", ctx.Err())
	}
	if got := source.Peer(source.PublicKey(), remote.PublicKey()); got != inbound {
		t.Fatalf("generic peer lookup = %p, want %p", got, inbound)
	}
	if got, lookupErr := source.OutboundPeer(
		source.PublicKey(),
		remote.PublicKey(),
	); got != nil || !errors.Is(lookupErr, ErrOutboundPeerNotFound) {
		t.Fatalf(
			"outbound lookup = (%p, %v), want not found",
			got,
			lookupErr,
		)
	}

	outbound, err := source.DialDefault(ctx, remote.PublicKey(), remoteAddr)
	if err != nil {
		t.Fatalf("create source outbound path: %v", err)
	}
	if outbound != inbound {
		t.Fatal("outbound connection did not attach to the existing managed path")
	}
	got, lookupErr := source.OutboundPeer(
		source.PublicKey(),
		remote.PublicKey(),
	)
	if lookupErr != nil {
		t.Fatalf("lookup outbound path: %v", lookupErr)
	}
	if got != outbound {
		t.Fatalf("outbound lookup = %p, want %p", got, outbound)
	}
}

func TestClientOnlyGatewayOutboundDialsIgnoreInboundConnectionLimits(t *testing.T) {
	firstServer, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	firstServer.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(context.Context, []byte) ([]byte, error) {
			return []byte("first"), nil
		})
		return nil
	})
	firstAddr := startGateway(t, firstServer)

	secondServer, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	secondServer.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(context.Context, []byte) ([]byte, error) {
			return []byte("second"), nil
		})
		return nil
	})
	secondAddr := startGateway(t, secondServer)

	limits := DefaultLimits()
	limits.MaxConnections = 1
	limits.MaxConnectionsPerIP = 1
	client, err := NewGatewayWithLimits(limits, mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	firstPeer, err := client.DialDefault(ctx, firstServer.PublicKey(), firstAddr)
	if err != nil {
		t.Fatalf("dial first outbound path: %v", err)
	}
	secondPeer, err := client.DialDefault(ctx, secondServer.PublicKey(), secondAddr)
	if err != nil {
		t.Fatalf("dial second outbound path: %v", err)
	}

	for name, peer := range map[string]*Peer{
		"first":  firstPeer,
		"second": secondPeer,
	} {
		answer, err := peer.Query(ctx, []byte(name), 0)
		if err != nil {
			t.Fatalf("query %s outbound path: %v", name, err)
		}
		if !bytes.Equal(answer, []byte(name)) {
			t.Fatalf("%s answer = %q, want %s", name, answer, name)
		}
	}
}

func TestServingGatewayOutboundDialIgnoresInboundConnectionLimits(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxConnections = 1
	limits.MaxConnectionsPerIP = 1
	source, err := NewGatewayWithLimits(limits, mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	sourceAddr := startGateway(t, source)

	target, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	target.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(context.Context, []byte) ([]byte, error) {
			return []byte("target"), nil
		})
		return nil
	})
	targetAddr := startGateway(t, target)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	inbound, err := Dial(ctx, sourceAddr, mustKey(t), source.PublicKey())
	if err != nil {
		t.Fatalf("fill inbound connection limit: %v", err)
	}
	defer inbound.Close()

	peer, err := source.DialDefault(ctx, target.PublicKey(), targetAddr)
	if err != nil {
		t.Fatalf("dial outbound while inbound limit is full: %v", err)
	}
	answer, err := peer.Query(ctx, []byte("outbound"), 0)
	if err != nil {
		t.Fatalf("query outbound path: %v", err)
	}
	if !bytes.Equal(answer, []byte("target")) {
		t.Fatalf("answer = %q, want target", answer)
	}
}

func TestPeerStrictOutboundOperationsRejectInboundOnlyPath(t *testing.T) {
	source, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	inboundPath := make(chan *Peer, 1)
	source.SetConnectionHandler(func(peer *Peer) error {
		inboundPath <- peer
		return nil
	})
	sourceAddr := startGateway(t, source)

	remote, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close() })

	var queryCalls atomic.Int32
	messages := make(chan []byte, 1)
	remote.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(context.Context, []byte) ([]byte, error) {
			queryCalls.Add(1)
			return []byte("reverse"), nil
		})
		peer.SetMessageHandler(func(_ context.Context, payload []byte) {
			messages <- payload
		})
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err = remote.DialDefault(ctx, source.PublicKey(), sourceAddr); err != nil {
		t.Fatalf("create source inbound path: %v", err)
	}

	var inbound *Peer
	select {
	case inbound = <-inboundPath:
	case <-ctx.Done():
		t.Fatalf("wait for source inbound path: %v", ctx.Err())
	}

	answer, err := inbound.QueryOutbound(ctx, []byte("strict query"), 0)
	if answer != nil || !errors.Is(err, ErrOutboundPeerNotFound) {
		t.Fatalf("strict query = (%q, %v), want outbound not found", answer, err)
	}
	if err = inbound.SendOutboundMessage(ctx, []byte("strict message")); !errors.Is(err, ErrOutboundPeerNotFound) {
		t.Fatalf("strict message error = %v, want outbound not found", err)
	}
	if got := queryCalls.Load(); got != 0 {
		t.Fatalf("reverse query handler calls = %d, want 0", got)
	}
	select {
	case payload := <-messages:
		t.Fatalf("reverse message handler received %q", payload)
	default:
	}

	answer, err = inbound.Query(ctx, []byte("regular query"), 0)
	if err != nil {
		t.Fatalf("query reverse path: %v", err)
	}
	if !bytes.Equal(answer, []byte("reverse")) {
		t.Fatalf("reverse answer = %q, want reverse", answer)
	}
	if err = inbound.SendMessage(ctx, []byte("regular message")); err != nil {
		t.Fatalf("message reverse path: %v", err)
	}
	select {
	case payload := <-messages:
		if !bytes.Equal(payload, []byte("regular message")) {
			t.Fatalf("reverse message = %q, want regular message", payload)
		}
	case <-ctx.Done():
		t.Fatalf("wait for reverse message: %v", ctx.Err())
	}
}

func TestGatewayConcurrentDialDefaultResolvedCallsResolverOnce(t *testing.T) {
	server, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	server.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(context.Context, []byte) ([]byte, error) {
			return []byte("resolved"), nil
		})
		return nil
	})
	addr := startGateway(t, server)

	client, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var resolverCalls atomic.Int32
	resolverEntered := make(chan struct{})
	releaseResolver := make(chan struct{})
	resolver := AddressResolver(func(ctx context.Context) (string, error) {
		if resolverCalls.Add(1) == 1 {
			close(resolverEntered)
		}

		select {
		case <-releaseResolver:
			return addr, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})

	type dialResult struct {
		peer *Peer
		err  error
	}

	const callers = 8
	results := make(chan dialResult, callers)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			ready.Done()
			<-start

			peer, err := client.DialDefaultResolved(ctx, server.PublicKey(), resolver)
			results <- dialResult{peer: peer, err: err}
		}()
	}
	ready.Wait()
	close(start)

	select {
	case <-resolverEntered:
	case <-ctx.Done():
		t.Fatalf("wait for resolver: %v", ctx.Err())
	}
	if got := resolverCalls.Load(); got != 1 {
		t.Fatalf("resolver calls while blocked = %d, want 1", got)
	}
	close(releaseResolver)

	var peer *Peer
	for i := 0; i < callers; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("resolved dial %d: %v", i, result.err)
		}
		if peer == nil {
			peer = result.peer
		} else if result.peer != peer {
			t.Fatalf("resolved dial %d peer = %p, want %p", i, result.peer, peer)
		}
	}
	if got := resolverCalls.Load(); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}

	answer, err := peer.QueryOutbound(ctx, []byte("query"), 0)
	if err != nil {
		t.Fatalf("query resolved outbound: %v", err)
	}
	if !bytes.Equal(answer, []byte("resolved")) {
		t.Fatalf("resolved answer = %q, want resolved", answer)
	}
}

func TestGatewayDialDefaultResolvedSkipsResolverForLiveOutbound(t *testing.T) {
	server, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("dial outbound: %v", err)
	}

	var resolverCalls atomic.Int32
	reused, err := client.DialDefaultResolved(
		ctx,
		server.PublicKey(),
		func(context.Context) (string, error) {
			resolverCalls.Add(1)
			return "", errors.New("resolver must not be called")
		},
	)
	if err != nil {
		t.Fatalf("reuse resolved outbound: %v", err)
	}
	if reused != peer {
		t.Fatalf("reused peer = %p, want %p", reused, peer)
	}
	if got := resolverCalls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
}
