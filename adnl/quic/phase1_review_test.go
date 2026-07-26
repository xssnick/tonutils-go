package quic

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	quicgo "github.com/xssnick/quic-go-ton"
)

func TestInboundPartialStreamTimeoutReleasesAdmission(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxConcurrentIncomingStreams = 1
	limits.MaxConcurrentIncomingStreamsPerConnection = 1
	limits.StreamReadTimeout = 100 * time.Millisecond

	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return payload, nil
		},
	}
	addr, server := startPhase1ReviewServer(t, handler, limits, mustKey(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	partial := openPhase1ReviewPartialQuery(t, ctx, client, 32)
	defer partial.CancelRead(0)
	defer partial.CancelWrite(0)

	waitPhase1ReviewCondition(t, time.Second, "partial stream admission", func() bool {
		streams, payloadBytes := phase1ReviewAdmissionUsage(server.admission)
		return streams == 1 && payloadBytes == 32
	})
	waitPhase1ReviewCondition(t, 2*time.Second, "timed-out stream admission release", func() bool {
		streams, payloadBytes := phase1ReviewAdmissionUsage(server.admission)
		return streams == 0 && payloadBytes == 0
	})

	answer, err := client.Query(ctx, []byte("after-timeout"), 0)
	if err != nil {
		t.Fatalf("query after partial stream timeout: %v", err)
	}
	if !bytes.Equal(answer, []byte("after-timeout")) {
		t.Fatalf("answer = %q, want after-timeout", answer)
	}
}

func TestPerConnectionAdmissionDoesNotConsumeGlobalQuota(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxConcurrentIncomingStreams = 2
	limits.MaxConcurrentIncomingStreamsPerConnection = 1
	limits.StreamReadTimeout = 5 * time.Second

	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return payload, nil
		},
	}
	addr, server := startPhase1ReviewServer(t, handler, limits, mustKey(t))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	attacker, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial first client: %v", err)
	}
	defer attacker.Close()

	first := openPhase1ReviewPartialQuery(t, ctx, attacker, 64)
	defer first.CancelRead(0)
	defer first.CancelWrite(0)

	waitPhase1ReviewCondition(t, time.Second, "first connection-local admission", func() bool {
		streams, payloadBytes := phase1ReviewAdmissionUsage(server.admission)
		return streams == 1 && payloadBytes == 64
	})

	backpressured := openPhase1ReviewPartialQuery(t, ctx, attacker, 64)
	defer backpressured.CancelRead(0)
	defer backpressured.CancelWrite(0)
	if err = backpressured.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set backpressured stream deadline: %v", err)
	}
	var one [1]byte
	if _, err = backpressured.Read(one[:]); err == nil {
		t.Fatal("second stream on the same connection was serviced over the limit")
	}
	var streamErr *quicgo.StreamError
	if errors.As(err, &streamErr) {
		t.Fatalf("second stream error = %v, want backpressure without cancellation", err)
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("second stream error = %v, want %v", err, os.ErrDeadlineExceeded)
	}

	streams, payloadBytes := phase1ReviewAdmissionUsage(server.admission)
	if streams != 1 || payloadBytes != 64 {
		t.Fatalf("global admission after connection-local backpressure = (%d streams, %d bytes), want (1, 64)",
			streams, payloadBytes)
	}

	legitimate, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial second client: %v", err)
	}
	defer legitimate.Close()

	answer, err := legitimate.Query(ctx, []byte("legitimate"), 0)
	if err != nil {
		t.Fatalf("query from second connection: %v", err)
	}
	if !bytes.Equal(answer, []byte("legitimate")) {
		t.Fatalf("answer = %q, want legitimate", answer)
	}

	waitPhase1ReviewCondition(t, time.Second, "legitimate lease release", func() bool {
		streams, payloadBytes = phase1ReviewAdmissionUsage(server.admission)
		return streams == 1 && payloadBytes == 64
	})
}

func TestGatewayInboundWatcherCleansPathWhileConnectionHandlerBlocked(t *testing.T) {
	handlerStarted := make(chan *Peer, 1)
	handlerRelease := make(chan struct{})
	handlerDone := make(chan struct{})
	disconnected := make(chan struct{}, 1)

	server, err := NewGateway(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	server.SetConnectionHandler(func(peer *Peer) error {
		peer.SetDisconnectHandler(func(peer *Peer) {
			disconnected <- struct{}{}
		})
		handlerStarted <- peer
		<-handlerRelease
		close(handlerDone)
		return nil
	})
	addr := startGateway(t, server)

	var releaseOnce sync.Once
	releaseHandler := func() {
		releaseOnce.Do(func() {
			close(handlerRelease)
		})
	}
	defer releaseHandler()

	clientKey := mustKey(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := Dial(ctx, addr, clientKey, server.PublicKey())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	var inbound *Peer
	select {
	case inbound = <-handlerStarted:
	case <-ctx.Done():
		_ = client.Close()
		t.Fatalf("wait for connection handler: %v", ctx.Err())
	}

	if err = client.Close(); err != nil {
		t.Fatalf("close client connection: %v", err)
	}

	waitPhase1ReviewCondition(t, 3*time.Second, "inbound watcher path cleanup", func() bool {
		return inbound.closed.Load() &&
			server.Peer(server.PublicKey(), clientKey.Public().(ed25519.PublicKey)) == nil
	})
	select {
	case <-disconnected:
		t.Fatal("disconnect handler ran before ConnectionHandler returned")
	default:
	}
	select {
	case <-handlerDone:
		t.Fatal("connection handler unexpectedly returned before it was released")
	default:
	}

	releaseHandler()
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("connection handler did not return after release")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("disconnect handler was not called after ConnectionHandler returned")
	}
}

func TestLargeStreamObjectsRemainCompatible(t *testing.T) {
	const payloadSize = 10 << 20

	largePayload := bytes.Repeat([]byte{0xA5}, payloadSize)
	messageResult := make(chan error, 1)

	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			switch {
			case bytes.Equal(payload, []byte("large-answer")):
				return largePayload, nil
			case bytes.Equal(payload, largePayload):
				return []byte("large-query-ok"), nil
			default:
				return nil, fmt.Errorf("unexpected query payload of %d bytes", len(payload))
			}
		},
		OnMessage: func(ctx context.Context, from ed25519.PublicKey, payload []byte) {
			if !bytes.Equal(payload, largePayload) {
				messageResult <- fmt.Errorf("message payload mismatch: got %d bytes", len(payload))
				return
			}
			messageResult <- nil
		},
	}

	limits := DefaultLimits()
	limits.MaxObjectSize = 12 << 20
	limits.MaxBufferedIncomingBytes = 24 << 20
	limits.MaxBufferedIncomingBytesPerConnection = 12 << 20
	addr, server := startPhase1ReviewServer(t, handler, limits, mustKey(t))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	_, _, _, answerLimit, err := boxedObjectHeader(idQuicAnswer, len(largePayload))
	if err != nil {
		t.Fatal(err)
	}
	answer, err := client.Query(ctx, []byte("large-answer"), int64(answerLimit))
	if err != nil {
		t.Fatalf("query with large answer: %v", err)
	}
	if !bytes.Equal(answer, largePayload) {
		t.Fatalf("large answer mismatch: got %d bytes", len(answer))
	}

	answer, err = client.Query(ctx, largePayload, 0)
	if err != nil {
		t.Fatalf("large query: %v", err)
	}
	if !bytes.Equal(answer, []byte("large-query-ok")) {
		t.Fatalf("large query answer = %q, want large-query-ok", answer)
	}

	if err = client.SendMessage(ctx, largePayload); err != nil {
		t.Fatalf("large message: %v", err)
	}
	select {
	case err = <-messageResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatalf("wait for large message: %v", ctx.Err())
	}
}

func TestPeerLastClientRemovalIsAtomicWithReconnect(t *testing.T) {
	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return payload, nil
		},
	}
	addr, server := startServer(t, handler, mustKey(t))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	liveClient, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer liveClient.Close()

	const iterations = 1000
	for i := 0; i < iterations; i++ {
		previous := &Client{}
		gateway := &Gateway{peers: make(map[pathKey]*Peer)}
		peer := &Peer{
			gateway: gateway,
			inbound: previous,
			ready:   make(chan struct{}),
		}
		gateway.peers[pathKey{}] = peer
		start := make(chan struct{})

		var removedLast bool
		var reconnected bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			removedLast = peer.removeClientAndCloseIfEmpty(previous, true)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, reconnected = peer.setOutbound(liveClient)
		}()
		close(start)
		wg.Wait()

		peer.mu.RLock()
		outbound := peer.outbound
		inbound := peer.inbound
		peer.mu.RUnlock()

		switch {
		case removedLast && !reconnected:
			if !peer.closed.Load() || inbound != nil || outbound != nil {
				t.Fatalf("iteration %d: removal won with inconsistent peer state", i)
			}
		case !removedLast && reconnected:
			if peer.closed.Load() || inbound != nil || outbound != liveClient {
				t.Fatalf("iteration %d: reconnect won with inconsistent peer state", i)
			}
		default:
			t.Fatalf("iteration %d: removal=%v reconnect=%v, want exactly one winner",
				i, removedLast, reconnected)
		}
	}
}

func TestGatewayDisconnectHandlerCanReenterClose(t *testing.T) {
	ready := make(chan struct{})
	close(ready)

	gateway := &Gateway{
		mode:      gatewayIdle,
		peers:     make(map[pathKey]*Peer),
		started:   make(chan struct{}),
		closeDone: make(chan struct{}),
	}
	peer := &Peer{
		gateway:   gateway,
		ready:     ready,
		initState: peerInitDone,
	}
	gateway.peers[peer.key] = peer

	reentrantResult := make(chan error, 1)
	peer.SetDisconnectHandler(func(peer *Peer) {
		reentrantResult <- gateway.Close()
	})

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- gateway.Close()
	}()

	select {
	case err := <-reentrantResult:
		if err != nil {
			t.Fatalf("reentrant Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect handler deadlocked reentering Gateway.Close")
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("outer Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("outer Gateway.Close did not return after reentrant disconnect handler")
	}
}

func TestGatewayClosedPeerRemainsTombstonedDuringDisconnect(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	peer, err := gateway.DialDefault(ctx, firstServer.defaultID.PublicKey(), firstAddr)
	if err != nil {
		t.Fatalf("dial first path: %v", err)
	}

	disconnectStarted := make(chan struct{})
	disconnectRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseDisconnect := func() {
		releaseOnce.Do(func() {
			close(disconnectRelease)
		})
	}
	defer releaseDisconnect()

	peer.SetDisconnectHandler(func(peer *Peer) {
		close(disconnectStarted)
		<-disconnectRelease
	})

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- peer.Close()
	}()

	select {
	case <-disconnectStarted:
	case <-ctx.Done():
		t.Fatalf("wait for disconnect handler: %v", ctx.Err())
	}

	if got := gateway.Peer(gateway.PublicKey(), firstServer.defaultID.PublicKey()); got != nil {
		t.Fatal("Peer exposed a closed path while its disconnect handler was blocked")
	}
	gateway.mu.RLock()
	tombstone := gateway.peers[peer.key]
	pathCount := len(gateway.peers)
	gateway.mu.RUnlock()
	if tombstone != peer || pathCount != 1 {
		t.Fatalf("closed path tombstone = %p with %d paths, want %p with 1 path",
			tombstone, pathCount, peer)
	}

	if _, err = gateway.DialDefault(ctx, firstServer.defaultID.PublicKey(), firstAddr); !errors.Is(err, ErrPeerClosed) {
		t.Fatalf("same-key dial while disconnect blocked error = %v, want %v", err, ErrPeerClosed)
	}
	if _, err = gateway.DialDefault(ctx, secondServer.defaultID.PublicKey(), secondAddr); !errors.Is(err, ErrPeerPathLimit) {
		t.Fatalf("different-key dial while tombstone holds slot error = %v, want %v",
			err, ErrPeerPathLimit)
	}

	releaseDisconnect()
	select {
	case err = <-closeResult:
		if err != nil {
			t.Fatalf("close first path: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("wait for first path close: %v", ctx.Err())
	}

	secondPeer, err := gateway.DialDefault(ctx, secondServer.defaultID.PublicKey(), secondAddr)
	if err != nil {
		t.Fatalf("dial after tombstone finalization: %v", err)
	}
	answer, err := secondPeer.Query(ctx, []byte("second"), 0)
	if err != nil {
		t.Fatalf("query after tombstone finalization: %v", err)
	}
	if !bytes.Equal(answer, []byte("second")) {
		t.Fatalf("answer = %q, want second", answer)
	}
}

func TestGatewayConcurrentCloseWaitsForPeerDetach(t *testing.T) {
	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return payload, nil
		},
	}
	addr, server := startServer(t, handler, mustKey(t))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	firstClient, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial first client: %v", err)
	}
	defer firstClient.Close()
	secondClient, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial second client: %v", err)
	}
	defer secondClient.Close()

	transportRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseTransport := func() {
		releaseOnce.Do(func() {
			close(transportRelease)
		})
	}
	defer releaseTransport()

	gateway := &Gateway{
		mode:        gatewayClientStarting,
		peers:       make(map[pathKey]*Peer),
		clientReady: transportRelease,
		started:     make(chan struct{}),
		closeDone:   make(chan struct{}),
	}
	firstReady := make(chan struct{})
	close(firstReady)
	firstKey := pathKey{}
	firstKey.local[0] = 1
	firstPeer := &Peer{
		gateway:   gateway,
		key:       firstKey,
		outbound:  firstClient,
		ready:     firstReady,
		initState: peerInitDone,
	}
	secondReady := make(chan struct{})
	close(secondReady)
	secondKey := pathKey{}
	secondKey.local[0] = 2
	secondPeer := &Peer{
		gateway:   gateway,
		key:       secondKey,
		inbound:   secondClient,
		ready:     secondReady,
		initState: peerInitDone,
	}
	gateway.peers[firstKey] = firstPeer
	gateway.peers[secondKey] = secondPeer

	const followers = 8
	closeResults := make(chan error, followers+1)
	go func() {
		closeResults <- gateway.Close()
	}()

	waitPhase1ReviewCondition(t, time.Second, "all peer detachment before transport close", func() bool {
		return phase1ReviewPeerClosedAndDetached(firstPeer) &&
			phase1ReviewPeerClosedAndDetached(secondPeer)
	})
	select {
	case <-gateway.closeDone:
		t.Fatal("closeDone closed before the blocked transport startup completed")
	default:
	}

	entered := make(chan struct{}, followers)
	for range followers {
		go func() {
			entered <- struct{}{}
			closeResults <- gateway.Close()
		}()
	}
	for range followers {
		<-entered
	}

	select {
	case err = <-closeResults:
		t.Fatalf("concurrent Close returned before closeDone: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	releaseTransport()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for range followers + 1 {
		select {
		case err = <-closeResults:
			if err != nil {
				t.Fatalf("concurrent Close: %v", err)
			}
			if !phase1ReviewPeerClosedAndDetached(firstPeer) ||
				!phase1ReviewPeerClosedAndDetached(secondPeer) {
				t.Fatal("Close returned before every peer was closed and detached")
			}
		case <-deadline.C:
			t.Fatal("concurrent Close callers remained blocked after closeDone")
		}
	}
}

func TestGatewayOutboundDialFailureDoesNotPromoteAttachedInbound(t *testing.T) {
	localKey := mustKey(t)
	remoteKey := mustKey(t)
	remotePublicKey := remoteKey.Public().(ed25519.PublicKey)

	handlerStarted := make(chan *Peer, 1)
	gateway, err := NewGateway(localKey)
	if err != nil {
		t.Fatal(err)
	}
	gateway.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			return payload, nil
		})
		handlerStarted <- peer
		return nil
	})
	gatewayAddr := startGateway(t, gateway)

	blackhole, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer blackhole.Close()
	if err = blackhole.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set blackhole read deadline: %v", err)
	}
	outboundPacket := make(chan error, 1)
	go func() {
		var packet [2048]byte
		_, _, readErr := blackhole.ReadFromUDP(packet[:])
		outboundPacket <- readErr
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outboundCtx, cancelOutbound := context.WithCancel(ctx)
	defer cancelOutbound()

	dialResult := make(chan phase1ReviewDialResult, 1)
	go func() {
		peer, dialErr := gateway.DialDefault(
			outboundCtx,
			remotePublicKey,
			blackhole.LocalAddr().String(),
		)
		dialResult <- phase1ReviewDialResult{peer: peer, err: dialErr}
	}()

	select {
	case err = <-outboundPacket:
		if err != nil {
			t.Fatalf("wait for outbound handshake packet: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("wait for outbound handshake packet: %v", ctx.Err())
	}

	localID, err := idFromPublicKey(gateway.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	remoteID, err := idFromPublicKey(remotePublicKey)
	if err != nil {
		t.Fatal(err)
	}
	key := pathKey{local: localID, peer: remoteID}
	gateway.mu.RLock()
	pendingPeer := gateway.peers[key]
	gateway.mu.RUnlock()
	if pendingPeer == nil {
		t.Fatal("outbound dial did not create a pending peer")
	}

	pendingPeer.mu.RLock()
	pending := pendingPeer.initState == peerInitPending &&
		pendingPeer.inbound == nil &&
		pendingPeer.outbound == nil
	pendingPeer.mu.RUnlock()
	if !pending {
		t.Fatal("outbound dial was not pending before the inbound path arrived")
	}

	inboundClient, err := Dial(ctx, gatewayAddr, remoteKey, gateway.PublicKey())
	if err != nil {
		t.Fatalf("dial inbound path: %v", err)
	}
	defer inboundClient.Close()

	waitPhase1ReviewCondition(t, time.Second, "same-key inbound attachment", func() bool {
		pendingPeer.mu.RLock()
		attached := pendingPeer.inbound != nil &&
			pendingPeer.outbound == nil &&
			pendingPeer.initState == peerInitPending
		pendingPeer.mu.RUnlock()
		return attached
	})
	select {
	case <-handlerStarted:
		t.Fatal("ConnectionHandler ran before the outbound attempt completed")
	default:
	}

	cancelOutbound()
	var result phase1ReviewDialResult
	select {
	case result = <-dialResult:
	case <-ctx.Done():
		t.Fatalf("wait for outbound dial result: %v", ctx.Err())
	}
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("outbound dial error = %v, want %v", result.err, context.Canceled)
	}
	if result.peer != nil {
		t.Fatalf("outbound dial returned inbound peer %p", result.peer)
	}

	select {
	case initializedPeer := <-handlerStarted:
		if initializedPeer != pendingPeer {
			t.Fatalf("initialized peer = %p, want %p", initializedPeer, pendingPeer)
		}
	case <-ctx.Done():
		t.Fatalf("wait for attached inbound peer initialization: %v", ctx.Err())
	}

	answer, err := inboundClient.Query(ctx, []byte("inbound"), 0)
	if err != nil {
		t.Fatalf("query over attached inbound path: %v", err)
	}
	if !bytes.Equal(answer, []byte("inbound")) {
		t.Fatalf("answer = %q, want inbound", answer)
	}
}

type phase1ReviewDialResult struct {
	peer *Peer
	err  error
}

func startPhase1ReviewServer(
	t *testing.T,
	handler Handler,
	limits Limits,
	keys ...ed25519.PrivateKey,
) (string, *Server) {
	t.Helper()

	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerWithLimits(handler, limits, keys...)
	if err != nil {
		_ = pc.Close()
		t.Fatal(err)
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(pc)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = server.WaitReady(ctx); err != nil {
		_ = server.Close()
		_ = pc.Close()
		t.Fatalf("wait for QUIC server: %v", err)
	}

	t.Cleanup(func() {
		_ = server.Close()
		_ = pc.Close()
		if err := <-serveErr; err != nil {
			t.Errorf("serve QUIC: %v", err)
		}
	})
	return pc.LocalAddr().String(), server
}

func openPhase1ReviewPartialQuery(
	t *testing.T,
	ctx context.Context,
	client *Client,
	payloadLen int,
) *quicgo.Stream {
	t.Helper()

	stream, err := client.conn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open partial query stream: %v", err)
	}
	header, headerLen, _, _, err := boxedObjectHeader(idQuicQuery, payloadLen)
	if err != nil {
		t.Fatalf("build partial query header: %v", err)
	}
	partial := make([]byte, headerLen+1)
	copy(partial, header[:headerLen])
	partial[headerLen] = 0x01
	if err = writeFull(stream, partial); err != nil {
		t.Fatalf("write partial query: %v", err)
	}
	return stream
}

func phase1ReviewAdmissionUsage(admission *streamAdmission) (uint64, uint64) {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	return admission.activeStreams, admission.reservedPayloadBytes.Load()
}

func phase1ReviewPeerClosedAndDetached(peer *Peer) bool {
	peer.mu.RLock()
	defer peer.mu.RUnlock()
	return peer.closed.Load() && peer.inbound == nil && peer.outbound == nil
}

func waitPhase1ReviewCondition(
	t *testing.T,
	timeout time.Duration,
	description string,
	condition func() bool,
) {
	t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", description)
		case <-ticker.C:
		}
	}
}
