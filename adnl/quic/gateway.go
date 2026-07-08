package quic

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	quicgo "github.com/xssnick/quic-go-ton"
)

// ErrNoQueryHandler is returned when a peer receives quic.query but has no handler.
var ErrNoQueryHandler = errors.New("quic: peer query handler is not set")

// ErrPeerClosed is returned when a peer has no live QUIC connection.
var ErrPeerClosed = errors.New("quic: peer is closed")

// ConnectionHandler is called once when a Gateway sees a new (local, peer) path.
type ConnectionHandler func(peer *Peer) error

// PeerQueryHandler handles inbound quic.query streams for a Peer.
type PeerQueryHandler func(ctx context.Context, payload []byte) ([]byte, error)

// PeerMessageHandler handles inbound quic.message streams for a Peer.
type PeerMessageHandler func(ctx context.Context, payload []byte)

// PeerDisconnectHandler is called after the path loses all live connections or is closed.
type PeerDisconnectHandler func(peer *Peer)

type pathKey struct {
	local adnlID
	peer  adnlID
}

// Gateway manages TON QUIC peers keyed by (local ADNL id, peer ADNL id).
type Gateway struct {
	defaultID  Identity
	ids        []Identity
	byID       map[adnlID]Identity
	maxObjSize int64

	mu     sync.RWMutex
	peers  map[pathKey]*Peer
	server *Server

	connHandler atomic.Pointer[ConnectionHandler]
}

// NewGateway builds a Gateway hosting the given local Ed25519 identities.
func NewGateway(keys ...ed25519.PrivateKey) (*Gateway, error) {
	if len(keys) == 0 {
		return nil, errors.New("quic: at least one identity key is required")
	}

	g := &Gateway{
		ids:        make([]Identity, 0, len(keys)),
		byID:       make(map[adnlID]Identity, len(keys)),
		maxObjSize: DefaultMaxObjectSize,
		peers:      make(map[pathKey]*Peer),
	}
	for i, key := range keys {
		id, err := NewIdentity(key)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			g.defaultID = id
		}
		g.ids = append(g.ids, id)
		g.byID[id.id] = id
	}
	return g, nil
}

// ID returns a copy of the default local ADNL id.
func (g *Gateway) ID() []byte {
	return g.defaultID.ID()
}

// PublicKey returns a copy of the default local Ed25519 public key.
func (g *Gateway) PublicKey() ed25519.PublicKey {
	return g.defaultID.PublicKey()
}

// Identities returns the local Ed25519 public keys hosted by the Gateway.
func (g *Gateway) Identities() []ed25519.PublicKey {
	keys := make([]ed25519.PublicKey, len(g.ids))
	for i, id := range g.ids {
		keys[i] = id.PublicKey()
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
	keys := make([]ed25519.PrivateKey, len(g.ids))
	for i, id := range g.ids {
		keys[i] = id.key
	}

	srv, err := NewServer(Handler{
		pathHandler: g,
	}, keys...)
	if err != nil {
		return err
	}
	srv.maxObjectSize = g.maxObjSize

	g.mu.Lock()
	if g.server != nil {
		g.mu.Unlock()
		return errors.New("quic: gateway server is already running")
	}
	g.server = srv
	g.mu.Unlock()

	err = srv.Serve(pc)

	g.mu.Lock()
	if g.server == srv {
		g.server = nil
	}
	g.mu.Unlock()

	return err
}

// Dial establishes or reuses a peer path from local to peer.
func (g *Gateway) Dial(ctx context.Context, local, peer ed25519.PublicKey, addr string) (*Peer, error) {
	localID, err := idFromPublicKey(local)
	if err != nil {
		return nil, err
	}
	peerID, err := idFromPublicKey(peer)
	if err != nil {
		return nil, err
	}

	id, ok := g.byID[localID]
	if !ok {
		return nil, fmt.Errorf("quic: unknown local identity %s", localID)
	}

	p, created := g.getOrCreatePeer(pathKey{local: localID, peer: peerID}, peer)
	if !created {
		if err := p.waitReady(ctx); err != nil {
			return nil, err
		}
		if p.client() != nil {
			return p, nil
		}
	}

	p.dialMu.Lock()
	if p.client() != nil {
		p.dialMu.Unlock()
		if created {
			g.finishPeerInit(p, nil)
		}
		return p, nil
	}

	client, err := dialIdentity(ctx, addr, id, peer)
	if err != nil {
		p.dialMu.Unlock()
		if created {
			g.finishPeerInit(p, err)
		}
		g.removePeer(p)
		return nil, err
	}
	old, changed := p.setOutbound(client)
	if !changed {
		p.dialMu.Unlock()
		_ = client.Close()
		if created {
			g.finishPeerInit(p, ErrPeerClosed)
		}
		g.removePeer(p)
		return nil, ErrPeerClosed
	}
	p.dialMu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	g.watchClient(p, client, false)
	go g.serveClientConn(p, client)

	if created {
		if err = g.initPeer(p); err != nil {
			p.Close()
			return nil, err
		}
	}
	return p, nil
}

// DialDefault establishes or reuses a peer path from the default local identity.
func (g *Gateway) DialDefault(ctx context.Context, peer ed25519.PublicKey, addr string) (*Peer, error) {
	return g.Dial(ctx, g.defaultID.PublicKey(), peer, addr)
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
	return p
}

// Close closes the Gateway server and all active peers.
func (g *Gateway) Close() error {
	g.mu.Lock()
	srv := g.server
	g.server = nil
	peers := make([]*Peer, 0, len(g.peers))
	for _, p := range g.peers {
		peers = append(peers, p)
	}
	g.peers = make(map[pathKey]*Peer)
	g.mu.Unlock()

	for _, p := range peers {
		p.closeOnly()
	}
	if srv != nil {
		return srv.Close()
	}
	return nil
}

func (g *Gateway) getOrCreatePeer(key pathKey, peerKey ed25519.PublicKey) (*Peer, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if p := g.peers[key]; p != nil {
		if !p.closed.Load() {
			return p, false
		}
		delete(g.peers, key)
	}
	p := &Peer{
		gateway: g,
		key:     key,
		peerKey: append(ed25519.PublicKey(nil), peerKey...),
		ready:   make(chan struct{}),
	}
	g.peers[key] = p
	return p, true
}

func (g *Gateway) initPeer(p *Peer) error {
	handler := g.connHandler.Load()
	var err error
	if handler != nil {
		err = (*handler)(p)
	}
	g.finishPeerInit(p, err)
	if err != nil {
		g.removePeer(p)
	}
	return err
}

func (g *Gateway) finishPeerInit(p *Peer, err error) {
	p.initOnce.Do(func() {
		p.initErr = err
		close(p.ready)
	})
}

func (g *Gateway) removePeer(p *Peer) {
	g.mu.Lock()
	if g.peers[p.key] == p {
		delete(g.peers, p.key)
	}
	g.mu.Unlock()
}

func (g *Gateway) registerInbound(ctx context.Context, local, peer adnlID, peerKey ed25519.PublicKey, conn *quicgo.Conn) (*Peer, error) {
	p, created := g.getOrCreatePeer(pathKey{local: local, peer: peer}, peerKey)
	old, client, changed := p.setInboundConn(conn, peerKey, peer, g.maxObjSize)
	if changed {
		if old != nil {
			_ = old.Close()
		}
		g.watchClient(p, client, true)
	}

	if created {
		if err := g.initPeer(p); err != nil {
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
		if p.removeClient(client, inbound) && p.empty() {
			g.removePeer(p)
			p.closeByRemote()
		}
	}()
}

func (g *Gateway) handlePath(ctx context.Context, path streamPath) (streamHandler, error) {
	return g.registerInbound(ctx, path.local, path.peer, path.peerKey, path.conn)
}

func (g *Gateway) serveClientConn(p *Peer, client *Client) {
	ctx := client.conn.Context()
	for {
		st, err := client.conn.AcceptStream(ctx)
		if err != nil {
			return
		}
		go g.serveClientStream(ctx, p, st)
	}
}

func (g *Gateway) serveClientStream(ctx context.Context, p *Peer, st *quicgo.Stream) {
	id, payload, err := readBoxedObject(st, g.maxObjSize)
	if err != nil {
		st.CancelRead(1)
		st.Close()
		return
	}

	switch id {
	case idQuicQuery:
		answer, herr := p.handleQuery(ctx, payload)
		if herr != nil {
			st.CancelWrite(1)
			return
		}
		if err = writeBoxedObject(st, idQuicAnswer, answer); err != nil {
			st.CancelWrite(1)
		}
	case idQuicMessage:
		p.handleMessage(ctx, payload)
		_ = st.Close()
	default:
		st.CancelRead(1)
		st.CancelWrite(1)
	}
}

// Peer is a managed TON QUIC path between one local id and one peer id.
type Peer struct {
	gateway *Gateway
	key     pathKey
	peerKey ed25519.PublicKey

	ready    chan struct{}
	initOnce sync.Once
	initErr  error

	mu         sync.RWMutex
	dialMu     sync.Mutex
	outbound   *Client
	inbound    *Client
	remoteAddr string

	closed atomic.Bool

	queryHandler      atomic.Pointer[PeerQueryHandler]
	messageHandler    atomic.Pointer[PeerMessageHandler]
	disconnectHandler atomic.Pointer[PeerDisconnectHandler]
}

// LocalID returns a copy of the local ADNL id for this path.
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
	client := p.client()
	if client == nil {
		return nil, ErrPeerClosed
	}
	return client.Query(ctx, payload, maxAnswer)
}

// SendMessage sends a fire-and-forget quic.message on the path.
func (p *Peer) SendMessage(ctx context.Context, payload []byte) error {
	if err := p.waitReady(ctx); err != nil {
		return err
	}
	client := p.client()
	if client == nil {
		return ErrPeerClosed
	}
	return client.SendMessage(ctx, payload)
}

// Close closes all live connections for this path.
func (p *Peer) Close() error {
	p.gateway.removePeer(p)
	return p.closeOnly()
}

func (p *Peer) closeOnly() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}

	inbound, outbound := p.detachClients()
	if inbound != nil {
		_ = inbound.Close()
	}
	if outbound != nil && outbound != inbound {
		_ = outbound.Close()
	}
	p.callDisconnect()
	return nil
}

func (p *Peer) closeByRemote() {
	if p.closed.CompareAndSwap(false, true) {
		p.callDisconnect()
	}
}

func (p *Peer) callDisconnect() {
	handler := p.disconnectHandler.Load()
	if handler != nil {
		go (*handler)(p)
	}
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

func (p *Peer) setInboundConn(conn *quicgo.Conn, peerKey ed25519.PublicKey, peer adnlID, maxObjectSize int64) (old *Client, client *Client, changed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() {
		return nil, nil, false
	}
	if p.inbound != nil && p.inbound.conn == conn {
		return nil, p.inbound, false
	}
	client = clientFromConn(conn, peerKey, peer, maxObjectSize)
	old = p.inbound
	p.inbound = client
	if p.remoteAddr == "" {
		p.remoteAddr = conn.RemoteAddr().String()
	}
	return old, client, true
}

func (p *Peer) detachClients() (*Client, *Client) {
	p.mu.Lock()
	inbound := p.inbound
	outbound := p.outbound
	p.inbound = nil
	p.outbound = nil
	p.mu.Unlock()
	return inbound, outbound
}

func (p *Peer) removeClient(client *Client, inbound bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if inbound {
		if p.inbound != client {
			return false
		}
		p.inbound = nil
		return true
	}
	if p.outbound != client {
		return false
	}
	p.outbound = nil
	return true
}

func (p *Peer) empty() bool {
	p.mu.RLock()
	empty := p.inbound == nil && p.outbound == nil
	p.mu.RUnlock()
	return empty
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
