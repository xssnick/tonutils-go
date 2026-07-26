package quic

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

const phase1AcceptanceTimeout = 15 * time.Second

func TestGatewayOutboundUsesListeningSourcePort(t *testing.T) {
	source, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	sourceAddr := startGateway(t, source)

	remoteAddr := make(chan string, 1)
	destination, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	destination.SetConnectionHandler(func(peer *Peer) error {
		remoteAddr <- peer.RemoteAddr()
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			return payload, nil
		})
		return nil
	})
	destinationAddr := startGateway(t, destination)

	ctx, cancel := context.WithTimeout(context.Background(), phase1AcceptanceTimeout)
	defer cancel()

	peer, err := source.DialDefault(ctx, destination.PublicKey(), destinationAddr)
	if err != nil {
		t.Fatalf("dial from listening gateway: %v", err)
	}
	answer, err := peer.Query(ctx, []byte("source-port"), 0)
	if err != nil {
		t.Fatalf("query from listening gateway: %v", err)
	}
	if !bytes.Equal(answer, []byte("source-port")) {
		t.Fatalf("answer = %q, want source-port", answer)
	}

	var observed string
	select {
	case observed = <-remoteAddr:
	case <-ctx.Done():
		t.Fatalf("wait for inbound path: %v", ctx.Err())
	}

	want, err := net.ResolveUDPAddr("udp", sourceAddr)
	if err != nil {
		t.Fatalf("resolve source gateway address: %v", err)
	}
	got, err := net.ResolveUDPAddr("udp", observed)
	if err != nil {
		t.Fatalf("resolve observed source address %q: %v", observed, err)
	}
	if got.Port != want.Port {
		t.Fatalf("outbound source port = %d, want listening port %d", got.Port, want.Port)
	}
}

func TestGatewayLateServeAfterClientOnlyDialReturnsModeError(t *testing.T) {
	server, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	server.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			return payload, nil
		})
		return nil
	})
	serverAddr := startGateway(t, server)

	client, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), phase1AcceptanceTimeout)
	defer cancel()

	peer, err := client.DialDefault(ctx, server.PublicKey(), serverAddr)
	if err != nil {
		t.Fatalf("client-only dial: %v", err)
	}
	if err = client.WaitReady(ctx); err != nil {
		t.Fatalf("wait for client-only transport: %v", err)
	}

	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	if err = client.Serve(pc); !errors.Is(err, ErrGatewayMode) {
		t.Fatalf("late Serve error = %v, want %v", err, ErrGatewayMode)
	}

	answer, err := peer.Query(ctx, []byte("still-live"), 0)
	if err != nil {
		t.Fatalf("query after rejected Serve: %v", err)
	}
	if !bytes.Equal(answer, []byte("still-live")) {
		t.Fatalf("answer = %q, want still-live", answer)
	}
}

func TestGatewayAddIdentityAfterServeConcurrentDial(t *testing.T) {
	target, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	target.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			return peer.LocalID(), nil
		})
		return nil
	})
	targetAddr := startGateway(t, target)
	targetDefaultKey := target.PublicKey()

	const workers = 8
	addedKeys := make([]ed25519.PrivateKey, workers)
	clientKeys := make([]ed25519.PrivateKey, workers)
	for i := range workers {
		addedKeys[i] = mustKey(t)
		clientKeys[i] = mustKey(t)
	}

	ctx, cancel := context.WithTimeout(context.Background(), phase1AcceptanceTimeout)
	defer cancel()

	start := make(chan struct{})
	errs := make(chan error, workers*2)
	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for i := range workers {
		addedKey := addedKeys[i]
		go func() {
			defer wg.Done()
			<-start

			if err := target.AddIdentity(addedKey); err != nil {
				errs <- fmt.Errorf("add identity: %w", err)
			}
		}()

		clientKey := clientKeys[i]
		go func() {
			defer wg.Done()
			<-start

			client, err := Dial(ctx, targetAddr, clientKey, targetDefaultKey)
			if err != nil {
				errs <- fmt.Errorf("concurrent dial: %w", err)
				return
			}
			defer client.Close()

			if _, err = client.Query(ctx, []byte("default"), 0); err != nil {
				errs <- fmt.Errorf("concurrent query: %w", err)
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}

	addedKey := addedKeys[len(addedKeys)-1]
	addedPublicKey := addedKey.Public().(ed25519.PublicKey)
	client, err := Dial(ctx, targetAddr, mustKey(t), addedPublicKey)
	if err != nil {
		t.Fatalf("dial identity added after Serve: %v", err)
	}
	defer client.Close()

	answer, err := client.Query(ctx, []byte("added"), 0)
	if err != nil {
		t.Fatalf("query identity added after Serve: %v", err)
	}
	addedID := adnlIDFromKey(addedPublicKey)
	if !bytes.Equal(answer, addedID[:]) {
		t.Fatalf("served local id = %x, want %x", answer, addedID[:])
	}
}

func TestQueryCancellationWithoutDeadlineReusesConnection(t *testing.T) {
	handlerStarted := make(chan struct{}, 1)
	handlerCanceled := make(chan error, 1)
	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			switch string(payload) {
			case "block":
				handlerStarted <- struct{}{}
				<-ctx.Done()
				handlerCanceled <- ctx.Err()
				return nil, ctx.Err()
			case "ping":
				return []byte("pong"), nil
			default:
				return nil, fmt.Errorf("unexpected query %q", payload)
			}
		},
	}

	serverKey := mustKey(t)
	addr, server := startServer(t, handler, serverKey)

	dialCtx, cancelDial := context.WithTimeout(context.Background(), phase1AcceptanceTimeout)
	defer cancelDial()
	client, err := Dial(dialCtx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	queryCtx, cancelQuery := context.WithCancel(context.Background())
	queryResult := make(chan error, 1)
	go func() {
		_, err := client.Query(queryCtx, []byte("block"), 0)
		queryResult <- err
	}()

	select {
	case <-handlerStarted:
	case <-time.After(phase1AcceptanceTimeout):
		t.Fatal("timed out waiting for query handler")
	}
	cancelQuery()

	select {
	case err = <-queryResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled query error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("query did not return promptly after cancellation")
	}

	select {
	case err = <-handlerCanceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("handler context error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server handler did not observe stream cancellation")
	}

	reuseCtx, cancelReuse := context.WithTimeout(context.Background(), phase1AcceptanceTimeout)
	defer cancelReuse()
	answer, err := client.Query(reuseCtx, []byte("ping"), 0)
	if err != nil {
		t.Fatalf("query after stream cancellation: %v", err)
	}
	if !bytes.Equal(answer, []byte("pong")) {
		t.Fatalf("answer after cancellation = %q, want pong", answer)
	}
}

func TestGatewayMaxPeerPaths(t *testing.T) {
	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return payload, nil
		},
	}
	firstAddr, firstServer := startServer(t, handler, mustKey(t))
	secondAddr, secondServer := startServer(t, handler, mustKey(t))

	limits := DefaultLimits()
	limits.MaxPeerPaths = 1
	gateway, err := NewGatewayWithLimits(limits, mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gateway.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), phase1AcceptanceTimeout)
	defer cancel()

	firstPeer, err := gateway.DialDefault(ctx, firstServer.defaultID.PublicKey(), firstAddr)
	if err != nil {
		t.Fatalf("dial first path: %v", err)
	}
	if _, err = gateway.DialDefault(ctx, secondServer.defaultID.PublicKey(), secondAddr); !errors.Is(err, ErrPeerPathLimit) {
		t.Fatalf("dial over path limit error = %v, want %v", err, ErrPeerPathLimit)
	}

	if err = firstPeer.Close(); err != nil {
		t.Fatalf("close first path: %v", err)
	}
	secondPeer, err := gateway.DialDefault(ctx, secondServer.defaultID.PublicKey(), secondAddr)
	if err != nil {
		t.Fatalf("dial after path release: %v", err)
	}
	answer, err := secondPeer.Query(ctx, []byte("second"), 0)
	if err != nil {
		t.Fatalf("query second path: %v", err)
	}
	if !bytes.Equal(answer, []byte("second")) {
		t.Fatalf("answer = %q, want second", answer)
	}
}

func TestConnectionLimiterEnforcesGlobalTotalAndPerIP(t *testing.T) {
	limiter := newConnectionLimiter(2, 1)
	firstAddr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1000}
	sameIPAddr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 2000}
	secondAddr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 2), Port: 1000}
	thirdAddr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 3), Port: 1000}

	releaseFirst, ok := limiter.acquire(firstAddr)
	if !ok {
		t.Fatal("first connection was rejected")
	}
	if _, ok = limiter.acquire(sameIPAddr); ok {
		t.Fatal("second connection from the same IP was admitted")
	}

	releaseSecond, ok := limiter.acquire(secondAddr)
	if !ok {
		t.Fatal("second connection from another IP was rejected")
	}
	if _, ok = limiter.acquire(thirdAddr); ok {
		t.Fatal("connection over the global total was admitted")
	}

	releaseFirst()
	releaseFirst()
	releaseThird, ok := limiter.acquire(thirdAddr)
	if !ok {
		t.Fatal("connection was rejected after an idempotent release")
	}

	releaseSecond()
	releaseThird()
}

func TestServerWaitReadyUnblocksWhenClosedBeforeServe(t *testing.T) {
	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return payload, nil
		},
	}
	server, err := NewServer(handler, mustKey(t))
	if err != nil {
		t.Fatal(err)
	}

	waiting := make(chan struct{})
	waitResult := make(chan error, 1)
	go func() {
		close(waiting)
		waitResult <- server.WaitReady(context.Background())
	}()
	<-waiting

	if err = server.Close(); err != nil {
		t.Fatalf("close before Serve: %v", err)
	}
	select {
	case err = <-waitResult:
		if !errors.Is(err, ErrServerClosed) {
			t.Fatalf("WaitReady error = %v, want %v", err, ErrServerClosed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitReady remained blocked after Close")
	}

	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if err = server.Serve(pc); !errors.Is(err, ErrServerClosed) {
		t.Fatalf("Serve after close error = %v, want %v", err, ErrServerClosed)
	}
}

func TestGatewayWaitReadyUnblocksWhenClosedBeforeServe(t *testing.T) {
	gateway, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}

	waiting := make(chan struct{})
	waitResult := make(chan error, 1)
	go func() {
		close(waiting)
		waitResult <- gateway.WaitReady(context.Background())
	}()
	<-waiting

	if err = gateway.Close(); err != nil {
		t.Fatalf("close before Serve: %v", err)
	}
	select {
	case err = <-waitResult:
		if !errors.Is(err, ErrGatewayClosed) {
			t.Fatalf("WaitReady error = %v, want %v", err, ErrGatewayClosed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitReady remained blocked after Close")
	}

	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if err = gateway.Serve(pc); !errors.Is(err, ErrGatewayClosed) {
		t.Fatalf("Serve after close error = %v, want %v", err, ErrGatewayClosed)
	}
}
