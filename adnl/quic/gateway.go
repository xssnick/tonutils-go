package quic

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	quicgo "github.com/xssnick/quic-go-ton"
)

var (
	// ErrNoQueryHandler is returned when a peer receives quic.query but has no handler.
	ErrNoQueryHandler = errors.New("quic: peer query handler is not set")
	// ErrPeerClosed is returned when a peer has no live QUIC connection.
	ErrPeerClosed = errors.New("quic: peer is closed")
	// ErrOutboundPeerNotFound is returned when a path has no live outbound connection.
	ErrOutboundPeerNotFound = errors.New("quic: outbound peer is not connected")
	// ErrGatewayClosed is returned after a Gateway has been closed.
	ErrGatewayClosed = errors.New("quic: gateway is closed")
	// ErrGatewayMode is returned when Serve follows a client-only Dial or more
	// than one Serve is attempted.
	ErrGatewayMode = errors.New("quic: gateway transport mode is already fixed")
	// ErrPeerPathLimit is returned when a Gateway has reached MaxPeerPaths.
	ErrPeerPathLimit = errors.New("quic: peer path limit reached")
)

// ConnectionHandler is called once when a Gateway sees a new (local, peer)
// path. It runs before incoming streams are accepted and should only install
// handlers and return without waiting on peer I/O.
type ConnectionHandler func(peer *Peer) error

// PeerQueryHandler handles inbound quic.query streams for a Peer.
type PeerQueryHandler func(ctx context.Context, payload []byte) ([]byte, error)

// PeerMessageHandler handles inbound quic.message streams for a Peer.
type PeerMessageHandler func(ctx context.Context, payload []byte)

// PeerDisconnectHandler is called after ConnectionHandler has returned and the
// path loses all live connections or is closed. It must return promptly: the
// closed path keeps its bounded MaxPeerPaths slot until this callback returns.
type PeerDisconnectHandler func(peer *Peer)

// AddressResolver resolves a remote QUIC address when an outbound connection
// must be established.
type AddressResolver func(ctx context.Context) (string, error)

type pathKey struct {
	local adnlID
	peer  adnlID
}

type gatewayMode uint8

const (
	gatewayIdle gatewayMode = iota
	gatewayServer
	gatewayClientStarting
	gatewayClient
	gatewayFailed
	gatewayClosed
)

// Gateway manages TON QUIC peers keyed by (local ADNL id, peer ADNL id).
type Gateway struct {
	limits     Limits
	identities *identityRegistry
	admission  *streamAdmission

	mu              sync.RWMutex
	peers           map[pathKey]*Peer
	mode            gatewayMode
	server          *Server
	clientTransport *clientTransport
	clientReady     chan struct{}
	transportErr    error
	started         chan struct{}
	startOnce       sync.Once
	closeDone       chan struct{}
	closeErr        error

	connHandler atomic.Pointer[ConnectionHandler]
}

// NewGateway builds a Gateway hosting the given local Ed25519 identities.
func NewGateway(keys ...ed25519.PrivateKey) (*Gateway, error) {
	return NewGatewayWithLimits(DefaultLimits(), keys...)
}

// NewGatewayWithLimits builds a Gateway with explicit per-instance resource
// limits.
func NewGatewayWithLimits(limits Limits, keys ...ed25519.PrivateKey) (*Gateway, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}

	identities, err := newIdentityRegistry(keys...)
	if err != nil {
		return nil, err
	}

	return &Gateway{
		limits:     limits,
		identities: identities,
		admission: newStreamAdmission(
			limits.MaxConcurrentIncomingStreams,
			limits.MaxBufferedIncomingBytes,
		),
		peers:     make(map[pathKey]*Peer),
		started:   make(chan struct{}),
		closeDone: make(chan struct{}),
	}, nil
}

func (g *Gateway) signalStarted() {
	g.startOnce.Do(func() {
		close(g.started)
	})
}

// AddIdentity makes an Ed25519 identity available to new paths and
// handshakes. Adding the same identity more than once is a no-op.
func (g *Gateway) AddIdentity(key ed25519.PrivateKey) error {
	_, err := g.identities.add(key)
	return err
}

// RemoveIdentity removes a non-default identity and closes all paths using it.
func (g *Gateway) RemoveIdentity(publicKey ed25519.PublicKey) error {
	id, err := idFromPublicKey(publicKey)
	if err != nil {
		return err
	}
	if err = g.identities.remove(id); err != nil {
		return err
	}

	g.mu.Lock()
	peers := make([]*Peer, 0)
	for key, peer := range g.peers {
		if key.local == id {
			delete(g.peers, key)
			peers = append(peers, peer)
		}
	}
	g.mu.Unlock()

	for _, peer := range peers {
		peer.closeWithError(ErrIdentityNotFound)
	}
	return nil
}

// ID returns a copy of the default local ADNL id.
func (g *Gateway) ID() []byte {
	return g.identities.defaultIdentity().ID()
}

// PublicKey returns a copy of the default local Ed25519 public key.
func (g *Gateway) PublicKey() ed25519.PublicKey {
	return g.identities.defaultIdentity().PublicKey()
}

// Identities returns the local Ed25519 public keys hosted by the Gateway.
func (g *Gateway) Identities() []ed25519.PublicKey {
	identities := g.identities.identities()
	keys := make([]ed25519.PublicKey, len(identities))
	for i, identity := range identities {
		keys[i] = identity.PublicKey()
	}
	return keys
}

// SetConnectionHandler sets the handler called for newly discovered paths.
func (g *Gateway) SetConnectionHandler(handler ConnectionHandler) {
	if handler == nil {
		g.connHandler.Store(nil)
		return
	}
	g.connHandler.Store(&handler)
}

// Serve listens on pc and accepts inbound TON QUIC connections.
func (g *Gateway) Serve(pc net.PacketConn) error {
	srv := newServer(Handler{}, g.limits, g.identities, g.admission, g)

	g.mu.Lock()
	if g.mode != gatewayIdle {
		err := ErrGatewayMode
		if g.mode == gatewayClosed {
			err = ErrGatewayClosed
		}
		g.mu.Unlock()
		return err
	}
	g.mode = gatewayServer
	g.server = srv
	g.signalStarted()
	g.mu.Unlock()

	err := srv.Serve(pc)
	closeErr := srv.Close()
	if err == nil {
		err = closeErr
	}

	g.mu.Lock()
	if g.mode != gatewayClosed && g.server == srv {
		g.mode = gatewayFailed
		g.transportErr = err
	}
	g.mu.Unlock()

	return err
}

// WaitReady waits until the Gateway has initialized its single UDP transport.
func (g *Gateway) WaitReady(ctx context.Context) error {
	select {
	case <-g.started:
	case <-ctx.Done():
		return ctx.Err()
	}

	for {
		g.mu.RLock()
		mode := g.mode
		server := g.server
		clientReady := g.clientReady
		err := g.transportErr
		g.mu.RUnlock()

		switch mode {
		case gatewayServer:
			if readyErr := server.WaitReady(ctx); readyErr != nil {
				return readyErr
			}

			g.mu.RLock()
			mode = g.mode
			err = g.transportErr
			g.mu.RUnlock()
			if mode == gatewayFailed {
				return err
			}
			return nil
		case gatewayClientStarting:
			select {
			case <-clientReady:
			case <-ctx.Done():
				return ctx.Err()
			}
		case gatewayClient:
			return nil
		case gatewayFailed:
			return err
		case gatewayClosed:
			return ErrGatewayClosed
		default:
			return ErrGatewayMode
		}
	}
}

// Dial establishes or reuses an outbound connection for the identity path
// from local to peer. addr is used only when the path has no live outbound.
func (g *Gateway) Dial(ctx context.Context, local, peer ed25519.PublicKey, addr string) (*Peer, error) {
	return g.DialResolved(ctx, local, peer, func(context.Context) (string, error) {
		return addr, nil
	})
}

// DialResolved establishes or reuses an outbound connection for the identity
// path from local to peer. resolve is called only when the path has no live
// outbound connection.
func (g *Gateway) DialResolved(
	ctx context.Context,
	local ed25519.PublicKey,
	peer ed25519.PublicKey,
	resolve AddressResolver,
) (*Peer, error) {
	localID, err := idFromPublicKey(local)
	if err != nil {
		return nil, err
	}
	peerID, err := idFromPublicKey(peer)
	if err != nil {
		return nil, err
	}

	identity, err := g.identities.get(localID)
	if err != nil {
		return nil, err
	}

	return g.dialResolved(ctx, identity, peerID, peer, resolve)
}

// Runs the connection handler for a path this call created, so every exit from a
// dial hands back a usable peer.
func (g *Gateway) readyPeer(p *Peer, created bool) (*Peer, error) {
	if !created {
		return p, nil
	}
	if err := g.initPeer(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Disposes of a path this call created after a failed dial: dropped if nothing
// else claimed it, otherwise it still needs its connection handler run.
func (g *Gateway) abandonDial(p *Peer, created bool, err error) error {
	if !created || p.closeIfNoClient(err) {
		return err
	}
	if initErr := g.initPeer(p); initErr != nil {
		return initErr
	}
	return err
}

func (g *Gateway) dialResolved(
	ctx context.Context,
	identity Identity,
	peerID adnlID,
	peerKey ed25519.PublicKey,
	resolve AddressResolver,
) (*Peer, error) {
	p, created, err := g.getOrCreatePeer(pathKey{local: identity.id, peer: peerID}, peerKey, false)
	if err != nil {
		return nil, err
	}
	if !created {
		if err := p.waitReady(ctx); err != nil {
			return nil, err
		}
	}

	// An established path has nothing to dial, and outboundClient takes its own
	// read lock. Reading it under dialMu instead put every send to a peer in line
	// behind every other send to it - the largest single source of mutex
	// contention on the node.
	if p.outboundClient() != nil {
		return g.readyPeer(p, created)
	}

	// dialMu serialises dial attempts only: one handshake per peer.
	p.dialMu.Lock()
	if p.outboundClient() != nil {
		p.dialMu.Unlock()
		return g.readyPeer(p, created)
	}

	addr, err := resolve(ctx)
	if err != nil {
		p.dialMu.Unlock()
		return nil, g.abandonDial(p, created, err)
	}

	client, err := g.dialIdentity(ctx, addr, identity, peerKey)
	if err != nil {
		p.dialMu.Unlock()
		return nil, g.abandonDial(p, created, err)
	}
	old, changed := p.setOutbound(client)
	if !changed {
		p.dialMu.Unlock()
		_ = client.Close()
		if created {
			p.closeWithError(ErrPeerClosed)
		}
		return nil, ErrPeerClosed
	}
	p.dialMu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	g.watchClient(p, client, false)
	if _, err = g.readyPeer(p, created); err != nil {
		return nil, err
	}

	go g.serveClientConn(p, client)
	return p, nil
}

func (g *Gateway) dialIdentity(ctx context.Context, addr string, local Identity, peer ed25519.PublicKey) (*Client, error) {
	for {
		g.mu.Lock()
		switch g.mode {
		case gatewayIdle:
			ready := make(chan struct{})
			g.mode = gatewayClientStarting
			g.clientReady = ready
			g.signalStarted()
			g.mu.Unlock()

			transport, err := newClientTransport(g.limits)

			g.mu.Lock()
			if g.mode == gatewayClientStarting {
				if err != nil {
					g.mode = gatewayFailed
					g.transportErr = err
				} else {
					g.mode = gatewayClient
					g.clientTransport = transport
				}
				close(ready)
				g.mu.Unlock()
			} else {
				g.mu.Unlock()
				if transport != nil {
					_ = transport.close()
				}
				close(ready)
			}
			if err != nil {
				return nil, err
			}
		case gatewayClientStarting:
			ready := g.clientReady
			g.mu.Unlock()
			select {
			case <-ready:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case gatewayClient:
			transport := g.clientTransport
			g.mu.Unlock()
			return transport.dialIdentity(ctx, addr, local, peer)
		case gatewayServer:
			server := g.server
			g.mu.Unlock()
			return server.dialIdentity(ctx, addr, local, peer)
		case gatewayFailed:
			err := g.transportErr
			g.mu.Unlock()
			if err == nil {
				err = ErrGatewayClosed
			}
			return nil, err
		case gatewayClosed:
			g.mu.Unlock()
			return nil, ErrGatewayClosed
		default:
			g.mu.Unlock()
			return nil, ErrGatewayMode
		}
	}
}

// DialDefault establishes or reuses a peer path from the default local identity.
func (g *Gateway) DialDefault(ctx context.Context, peer ed25519.PublicKey, addr string) (*Peer, error) {
	return g.DialDefaultResolved(ctx, peer, func(context.Context) (string, error) {
		return addr, nil
	})
}

// DialDefaultResolved establishes or reuses a peer path from the default local
// identity, resolving the remote address only when a dial is required.
func (g *Gateway) DialDefaultResolved(
	ctx context.Context,
	peer ed25519.PublicKey,
	resolve AddressResolver,
) (*Peer, error) {
	peerID, err := idFromPublicKey(peer)
	if err != nil {
		return nil, err
	}
	return g.dialResolved(ctx, g.identities.defaultIdentity(), peerID, peer, resolve)
}

// DialDefaultResolvedID is DialDefaultResolved for callers that already know the
// peer's 32-byte ADNL short id. peerKey is still required to authenticate the
// raw-public-key TLS handshake, and peerID must be its ADNL short id.
func (g *Gateway) DialDefaultResolvedID(
	ctx context.Context,
	peerID []byte,
	peerKey ed25519.PublicKey,
	resolve AddressResolver,
) (*Peer, error) {
	if len(peerID) != len(adnlID{}) {
		return nil, fmt.Errorf("quic: invalid ADNL id size %d", len(peerID))
	}
	if len(peerKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("quic: invalid Ed25519 public key size %d", len(peerKey))
	}
	return g.dialResolved(ctx, g.identities.defaultIdentity(), adnlID(peerID), peerKey, resolve)
}

// Peer returns the currently known peer for a path, if any.
func (g *Gateway) Peer(local, peer ed25519.PublicKey) *Peer {
	localID, err := idFromPublicKey(local)
	if err != nil {
		return nil
	}
	peerID, err := idFromPublicKey(peer)
	if err != nil {
		return nil
	}

	g.mu.RLock()
	p := g.peers[pathKey{local: localID, peer: peerID}]
	g.mu.RUnlock()
	if p != nil && p.closed.Load() {
		return nil
	}
	return p
}

// OutboundPeer returns the currently known peer when its path has a live
// outbound connection.
func (g *Gateway) OutboundPeer(
	local ed25519.PublicKey,
	peer ed25519.PublicKey,
) (*Peer, error) {
	localID, err := idFromPublicKey(local)
	if err != nil {
		return nil, err
	}
	peerID, err := idFromPublicKey(peer)
	if err != nil {
		return nil, err
	}

	g.mu.RLock()
	p := g.peers[pathKey{local: localID, peer: peerID}]
	g.mu.RUnlock()
	if p == nil || p.outboundClient() == nil {
		return nil, ErrOutboundPeerNotFound
	}
	return p, nil
}

// OutboundPeerDefaultID is OutboundPeer for the default local identity, keyed
// by the peer's precomputed 32-byte ADNL short id so neither public key is
// hashed per call.
func (g *Gateway) OutboundPeerDefaultID(peerID []byte) (*Peer, error) {
	if len(peerID) != len(adnlID{}) {
		return nil, fmt.Errorf("quic: invalid ADNL id size %d", len(peerID))
	}
	key := pathKey{
		local: g.identities.defaultIdentity().id,
		peer:  adnlID(peerID),
	}
	g.mu.RLock()
	p := g.peers[key]
	g.mu.RUnlock()
	if p == nil || p.outboundClient() == nil {
		return nil, ErrOutboundPeerNotFound
	}
	return p, nil
}

// Close closes the Gateway server and all active peers.
func (g *Gateway) Close() error {
	g.mu.Lock()
	if g.mode == gatewayClosed {
		done := g.closeDone
		g.mu.Unlock()
		<-done
		return g.closeErr
	}

	clientReady := g.clientReady
	srv := g.server
	clientTransport := g.clientTransport
	g.mode = gatewayClosed
	g.signalStarted()

	peers := make([]*Peer, 0, len(g.peers))
	for _, p := range g.peers {
		peers = append(peers, p)
	}
	g.peers = make(map[pathKey]*Peer)
	g.mu.Unlock()

	deferredPeers := make([]*Peer, 0, len(peers))
	for _, p := range peers {
		if transitioned, _ := p.beginCloseWithError(ErrGatewayClosed, true); transitioned {
			deferredPeers = append(deferredPeers, p)
		}
	}

	var errs []error
	if srv != nil {
		if err := srv.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if clientTransport != nil {
		if err := clientTransport.close(); err != nil {
			errs = append(errs, err)
		}
	}
	if clientReady != nil && clientTransport == nil {
		<-clientReady
	}
	err := errors.Join(errs...)

	g.mu.Lock()
	g.closeErr = err
	close(g.closeDone)
	g.mu.Unlock()

	for _, p := range deferredPeers {
		p.releaseDeferredFinalize()
	}
	return err
}

// inbound says who initiated the creation: paths live in one shared table, so
// without the distinction a remote party can fill it with connections it opens
// and leave us unable to dial anyone.
func (g *Gateway) getOrCreatePeer(key pathKey, peerKey ed25519.PublicKey, inbound bool) (*Peer, bool, error) {
	g.mu.RLock()
	if g.mode != gatewayClosed && g.mode != gatewayFailed {
		if p := g.peers[key]; p != nil && !p.closed.Load() {
			g.mu.RUnlock()
			return p, false, nil
		}
	}
	g.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.mode == gatewayClosed {
		return nil, false, ErrGatewayClosed
	}
	if g.mode == gatewayFailed {
		if g.transportErr != nil {
			return nil, false, g.transportErr
		}
		return nil, false, ErrGatewayClosed
	}
	if p := g.peers[key]; p != nil {
		if p.closed.Load() {
			return nil, false, ErrPeerClosed
		}
		return p, false, nil
	}
	if !g.identities.has(key.local) {
		return nil, false, fmt.Errorf("%w: %s", ErrIdentityNotFound, key.local)
	}
	limit := g.limits.MaxPeerPaths
	if inbound {
		// The reserved slots keep our own dialing alive under a connection flood.
		limit = g.limits.inboundPeerPathBudget()
	}
	if len(g.peers) >= limit {
		return nil, false, ErrPeerPathLimit
	}

	p := &Peer{
		gateway: g,
		key:     key,
		peerKey: append(ed25519.PublicKey(nil), peerKey...),
		ready:   make(chan struct{}),
	}
	g.peers[key] = p
	return p, true, nil
}

func (g *Gateway) initPeer(p *Peer) error {
	p.mu.Lock()
	switch p.initState {
	case peerInitRunning:
		ready := p.ready
		p.mu.Unlock()
		<-ready
		return p.initErr
	case peerInitDone:
		err := p.initErr
		p.mu.Unlock()
		return err
	}
	p.initState = peerInitRunning
	p.mu.Unlock()

	var err error
	if !g.identities.has(p.key.local) {
		err = ErrIdentityNotFound
	}

	if err == nil {
		p.mu.Lock()
		switch {
		case p.closed.Load():
			err = p.initAbort
			if err == nil {
				err = ErrPeerClosed
			}
		case p.inbound == nil && p.outbound == nil:
			err = ErrPeerClosed
		}
		// The linearization point for starting ConnectionHandler: a concurrent close
		// either wins above or records initAbort and waits for the handler result.
		p.mu.Unlock()
	}

	handler := g.connHandler.Load()
	if err == nil && handler != nil {
		err = (*handler)(p)
	}

	p.mu.Lock()
	if p.initAbort != nil {
		err = p.initAbort
	}
	p.initState = peerInitDone
	p.initErr = err
	close(p.ready)
	finalize := p.closed.Load() && !p.deferFinalize
	p.mu.Unlock()

	if finalize {
		p.finalizeClose()
	}
	if err != nil {
		p.closeWithError(err)
	}
	return err
}

func (g *Gateway) removePeer(p *Peer) {
	g.mu.Lock()
	if g.peers[p.key] == p {
		delete(g.peers, p.key)
	}
	g.mu.Unlock()
}

func (g *Gateway) registerInbound(ctx context.Context, local, peer adnlID, peerKey ed25519.PublicKey, conn *quicgo.Conn) (*Peer, error) {
	p, created, err := g.getOrCreatePeer(pathKey{local: local, peer: peer}, peerKey, true)
	if err != nil {
		return nil, err
	}
	if !g.identities.has(local) {
		p.closeWithError(ErrIdentityNotFound)
		return nil, ErrIdentityNotFound
	}

	old, client, err := p.attachInboundConn(conn, peerKey, peer, g.limits.MaxObjectSize)
	if err != nil {
		return nil, err
	}
	g.watchClient(p, client, true)
	if old != nil {
		_ = old.Close()
	}

	if created {
		if err := g.initPeer(p); err != nil {
			p.closeWithError(err)
			return nil, err
		}
	} else if err := p.waitReady(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (g *Gateway) watchClient(p *Peer, client *Client, inbound bool) {
	go func() {
		<-client.conn.Context().Done()
		p.removeClientAndCloseIfEmpty(client, inbound)
	}()
}

func (g *Gateway) handlePath(
	ctx context.Context,
	local, peer adnlID,
	peerKey ed25519.PublicKey,
	conn *quicgo.Conn,
) (streamHandler, error) {
	return g.registerInbound(ctx, local, peer, peerKey, conn)
}

func (g *Gateway) serveClientConn(p *Peer, client *Client) {
	// No receiver-wide scope: we dialed this connection, so its inbound streams
	// must not queue behind a flood of connections somebody else opened to us. The
	// per-connection scope still applies. Same rule as quic-server.cpp:653.
	serveConnStreams(client.conn.Context(), client.conn, nil, g.limits, p)
}

type peerInitState uint8

const (
	peerInitPending peerInitState = iota
	peerInitRunning
	peerInitDone
)

// Peer is a managed TON QUIC path between one local id and one peer id.
type Peer struct {
	gateway *Gateway
	key     pathKey
	peerKey ed25519.PublicKey

	mu            sync.RWMutex
	dialMu        sync.Mutex
	outbound      *Client
	inbound       *Client
	remoteAddr    string
	ready         chan struct{}
	initState     peerInitState
	initErr       error
	initAbort     error
	deferFinalize bool

	closed       atomic.Bool
	finalizeOnce sync.Once
	// QUIC keep-alive is shorter than the idle timeout, so a path never expires on
	// its own. Callers that bound their dialed set need this to tell a working path
	// from one that is merely kept alive.
	lastOutboundAt atomic.Int64

	queryHandler      atomic.Pointer[PeerQueryHandler]
	messageHandler    atomic.Pointer[PeerMessageHandler]
	disconnectHandler atomic.Pointer[PeerDisconnectHandler]
}

func (p *Peer) noteOutbound() {
	p.lastOutboundAt.Store(time.Now().UnixNano())
}

// LastOutbound reports when a payload was last pushed through this path, or the
// zero time if none ever was.
func (p *Peer) LastOutbound() time.Time {
	at := p.lastOutboundAt.Load()
	if at == 0 {
		return time.Time{}
	}
	return time.Unix(0, at)
}

func (p *Peer) LocalID() []byte {
	return p.key.local.bytes()
}

// PeerID returns a copy of the remote ADNL id for this path.
func (p *Peer) PeerID() []byte {
	return p.key.peer.bytes()
}

// PeerKey returns a copy of the remote Ed25519 public key for this path.
func (p *Peer) PeerKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), p.peerKey...)
}

// RemoteAddr returns the last known remote QUIC address.
func (p *Peer) RemoteAddr() string {
	p.mu.RLock()
	addr := p.remoteAddr
	p.mu.RUnlock()
	return addr
}

// SetQueryHandler sets the handler for inbound quic.query streams.
func (p *Peer) SetQueryHandler(handler PeerQueryHandler) {
	if handler == nil {
		p.queryHandler.Store(nil)
		return
	}
	p.queryHandler.Store(&handler)
}

// SetMessageHandler sets the handler for inbound quic.message streams.
func (p *Peer) SetMessageHandler(handler PeerMessageHandler) {
	if handler == nil {
		p.messageHandler.Store(nil)
		return
	}
	p.messageHandler.Store(&handler)
}

// SetDisconnectHandler sets the handler called when the path is closed.
func (p *Peer) SetDisconnectHandler(handler PeerDisconnectHandler) {
	if handler == nil {
		p.disconnectHandler.Store(nil)
		return
	}
	p.disconnectHandler.Store(&handler)
}

// Query sends a quic.query on the path and returns the answer payload.
func (p *Peer) Query(ctx context.Context, payload []byte, maxAnswer int64) ([]byte, error) {
	if err := p.waitReady(ctx); err != nil {
		return nil, err
	}
	p.noteOutbound()
	client := p.client()
	if client == nil {
		return nil, ErrPeerClosed
	}
	return client.Query(ctx, payload, maxAnswer)
}

// QueryOutbound sends a quic.query using only the outbound connection.
func (p *Peer) QueryOutbound(ctx context.Context, payload []byte, maxAnswer int64) ([]byte, error) {
	if err := p.waitReady(ctx); err != nil {
		return nil, err
	}
	p.noteOutbound()
	client := p.outboundClient()
	if client == nil {
		return nil, ErrOutboundPeerNotFound
	}
	return client.Query(ctx, payload, maxAnswer)
}

// SendMessage sends a fire-and-forget quic.message on the path.
func (p *Peer) SendMessage(ctx context.Context, payload []byte) error {
	if err := p.waitReady(ctx); err != nil {
		return err
	}
	p.noteOutbound()
	client := p.client()
	if client == nil {
		return ErrPeerClosed
	}
	return client.SendMessage(ctx, payload)
}

// SendOutboundMessage sends a quic.message using only the outbound connection.
func (p *Peer) SendOutboundMessage(ctx context.Context, payload []byte) error {
	if err := p.waitReady(ctx); err != nil {
		return err
	}
	p.noteOutbound()
	client := p.outboundClient()
	if client == nil {
		return ErrOutboundPeerNotFound
	}
	return client.SendMessage(ctx, payload)
}

// Close closes all live connections for this path.
func (p *Peer) Close() error {
	p.closeWithError(ErrPeerClosed)
	return nil
}

func (p *Peer) closeWithError(initErr error) {
	if _, finalize := p.beginCloseWithError(initErr, false); finalize {
		p.finalizeClose()
	}
}

// beginCloseWithError reports whether this call performed the close transition
// and whether the caller owes the finalize.
func (p *Peer) beginCloseWithError(initErr error, deferFinalize bool) (transitioned, finalize bool) {
	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		return false, false
	}
	p.closed.Store(true)
	p.deferFinalize = deferFinalize

	inbound := p.inbound
	outbound := p.outbound
	p.inbound = nil
	p.outbound = nil
	finalize = p.abortInitLocked(initErr)
	p.mu.Unlock()

	if inbound != nil {
		_ = inbound.Close()
	}
	if outbound != nil && outbound != inbound {
		_ = outbound.Close()
	}
	return true, finalize && !deferFinalize
}

func (p *Peer) releaseDeferredFinalize() {
	p.mu.Lock()
	p.deferFinalize = false
	finalize := p.closed.Load() && p.initState == peerInitDone
	p.mu.Unlock()

	if finalize {
		p.finalizeClose()
	}
}

func (p *Peer) closeIfNoClient(initErr error) bool {
	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		return true
	}
	if p.inbound != nil || p.outbound != nil {
		p.mu.Unlock()
		return false
	}
	p.closed.Store(true)
	finalize := p.abortInitLocked(initErr)
	p.mu.Unlock()

	if finalize {
		p.finalizeClose()
	}
	return true
}

func (p *Peer) abortInitLocked(err error) bool {
	switch p.initState {
	case peerInitPending:
		p.initState = peerInitDone
		p.initErr = err
		close(p.ready)
		return true
	case peerInitRunning:
		if p.initAbort == nil {
			p.initAbort = err
		}
		return false
	default:
		return true
	}
}

func (p *Peer) finalizeClose() {
	p.finalizeOnce.Do(func() {
		handler := p.disconnectHandler.Load()
		if handler != nil {
			(*handler)(p)
		}
		p.gateway.removePeer(p)
	})
}

func (p *Peer) waitReady(ctx context.Context) error {
	select {
	case <-p.ready:
		return p.initErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Peer) client() *Client {
	if p.closed.Load() {
		return nil
	}

	p.mu.RLock()
	client := p.outbound
	if client == nil {
		client = p.inbound
	}
	p.mu.RUnlock()
	return client
}

func (p *Peer) outboundClient() *Client {
	if p.closed.Load() {
		return nil
	}

	p.mu.RLock()
	client := p.outbound
	if client != nil && client.conn.Context().Err() != nil {
		client = nil
	}
	p.mu.RUnlock()
	return client
}

func (p *Peer) setOutbound(client *Client) (old *Client, changed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() {
		return nil, false
	}
	if p.outbound != nil && p.outbound.conn == client.conn {
		return nil, false
	}
	old = p.outbound
	p.outbound = client
	p.remoteAddr = client.conn.RemoteAddr().String()
	return old, true
}

func (p *Peer) attachInboundConn(conn *quicgo.Conn, peerKey ed25519.PublicKey, peer adnlID, maxObjectSize int64) (old *Client, client *Client, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() {
		return nil, nil, ErrPeerClosed
	}
	if p.inbound != nil && p.inbound.conn == conn {
		return nil, nil, errors.New("quic: inbound connection is already attached")
	}
	client = &Client{conn: conn, peerKey: peerKey, peer: peer, maxObjectSize: maxObjectSize}
	old = p.inbound
	p.inbound = client
	if p.remoteAddr == "" {
		p.remoteAddr = conn.RemoteAddr().String()
	}
	return old, client, nil
}

func (p *Peer) removeClientAndCloseIfEmpty(client *Client, inbound bool) bool {
	p.mu.Lock()

	if inbound {
		if p.inbound != client {
			p.mu.Unlock()
			return false
		}
		p.inbound = nil
	} else {
		if p.outbound != client {
			p.mu.Unlock()
			return false
		}
		p.outbound = nil
	}
	if p.inbound != nil || p.outbound != nil || p.closed.Load() {
		p.mu.Unlock()
		return false
	}

	p.closed.Store(true)
	finalize := p.abortInitLocked(ErrPeerClosed)
	p.mu.Unlock()
	if finalize {
		p.finalizeClose()
	}
	return true
}

func (p *Peer) handleQuery(ctx context.Context, payload []byte) ([]byte, error) {
	if err := p.waitReady(ctx); err != nil {
		return nil, err
	}
	handler := p.queryHandler.Load()
	if handler == nil {
		return nil, ErrNoQueryHandler
	}
	return (*handler)(ctx, payload)
}

func (p *Peer) handleMessage(ctx context.Context, payload []byte) {
	if err := p.waitReady(ctx); err != nil {
		return
	}
	handler := p.messageHandler.Load()
	if handler != nil {
		(*handler)(ctx, payload)
	}
}
