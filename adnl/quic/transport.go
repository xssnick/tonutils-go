// Package quic implements the TON QUIC transport: RFC 7250 raw-public-key
// (Ed25519/ADNL) mutual authentication over QUIC v1, with the
// quic.message/quic.query/quic.answer framing used by the reference C++ node.
//
// It is built on github.com/xssnick/quic-go-ton — a fork of quic-go bundled
// with an RFC 7250-patched crypto/tls — so the whole stack stays pure Go.
package quic

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	quicgo "github.com/xssnick/quic-go-ton"
	forktls "github.com/xssnick/quic-go-ton/tls"
)

// ALPN is the application protocol negotiated for TON QUIC.
const ALPN = "ton"

// DefaultMaxObjectSize is the whole-stream cap for unsolicited incoming
// objects, outgoing messages and query requests, and query answers without an
// explicit maxAnswer. It is independent of QUIC flow-control windows, which
// grow incrementally while an admitted object is read.
const DefaultMaxObjectSize = defaultMaxBoxedObjectSize

const (
	// The reference node splits this in two (quic-pimpl.h): 4 MiB for streams it
	// opens, 256 KiB for streams the peer opens. quic-go has a single knob for
	// both, so taking the larger keeps neither direction tighter than C++; the
	// per-connection window stays at C++'s DEFAULT_MAX_WINDOW of 24 MiB.
	defaultInitialStreamReceiveWindow     = 4 << 20
	defaultMaxStreamReceiveWindow         = 6 << 20
	defaultInitialConnectionReceiveWindow = 4 << 20
	defaultMaxConnectionReceiveWindow     = 24 << 20
	directWriteObjectThreshold            = 32 << 10
)

// Handler processes inbound stream objects for a Server.
type Handler struct {
	// OnQuery handles a quic.query and returns the answer payload. Required.
	OnQuery func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error)
	// OnMessage handles a fire-and-forget quic.message. Optional.
	OnMessage func(ctx context.Context, from ed25519.PublicKey, payload []byte)
}

type streamHandler interface {
	handleQuery(ctx context.Context, payload []byte) ([]byte, error)
	handleMessage(ctx context.Context, payload []byte)
}

var (
	// ErrServerClosed is returned when a QUIC server is closed before or while
	// an operation waits for it to start.
	ErrServerClosed = errors.New("quic: server is closed")

	errTooManyConnections             = errors.New("quic: too many active connections from remote address")
	errNewConnectionRateLimited       = errors.New("quic: new connection rate limit exceeded")
	errGlobalNewConnectionRateLimited = errors.New("quic: global new connection rate limit exceeded")
	errConnectionRateLimiterFull      = errors.New("quic: connection rate limiter source table is full")
)

type connectionLimiter struct {
	mu sync.Mutex

	maxTotal int
	maxPerIP int
	total    int
	active   map[remoteAddressKey]int
}

func newConnectionLimiter(maxTotal, maxPerIP int) *connectionLimiter {
	return &connectionLimiter{
		maxTotal: maxTotal,
		maxPerIP: maxPerIP,
		active:   make(map[remoteAddressKey]int),
	}
}

func (l *connectionLimiter) acquire(addr net.Addr) (func(), bool) {
	key := remoteIPKey(addr)

	l.mu.Lock()
	if l.total >= l.maxTotal || l.active[key] >= l.maxPerIP {
		l.mu.Unlock()
		return nil, false
	}
	l.total++
	l.active[key]++
	l.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.total--
			if l.active[key] <= 1 {
				delete(l.active, key)
			} else {
				l.active[key]--
			}
			l.mu.Unlock()
		})
	}, true
}

type remoteAddressKey struct {
	addr     netip.Addr
	fallback string
}

func remoteIPKey(addr net.Addr) remoteAddressKey {
	if udp, ok := addr.(*net.UDPAddr); ok {
		ip := udp.AddrPort().Addr()
		if ip.IsValid() {
			return remoteAddressKey{addr: ip.Unmap()}
		}
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err == nil {
		if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
			return remoteAddressKey{addr: ip.Unmap()}
		}
		return remoteAddressKey{fallback: host}
	}
	return remoteAddressKey{fallback: addr.String()}
}

// defaultQUICConfig returns the default quic-go settings for TON connections.
func defaultQUICConfig(limits Limits) *quicgo.Config {
	return &quicgo.Config{
		Versions:                       []quicgo.Version{quicgo.Version1},
		MaxIdleTimeout:                 15 * time.Second,
		KeepAlivePeriod:                5 * time.Second,
		InitialStreamReceiveWindow:     defaultInitialStreamReceiveWindow,
		MaxStreamReceiveWindow:         defaultMaxStreamReceiveWindow,
		InitialConnectionReceiveWindow: defaultInitialConnectionReceiveWindow,
		MaxConnectionReceiveWindow:     defaultMaxConnectionReceiveWindow,
		MaxIncomingStreams:             limits.MaxIncomingStreams,
		MaxIncomingUniStreams:          -1,
	}
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// Server accepts TON QUIC connections on a net.PacketConn and dispatches
// inbound query/message streams to a Handler.
type Server struct {
	handler Handler
	// paths, when set, hands every new connection to the Gateway that owns it
	// so the path can be reused for outbound traffic.
	paths     *Gateway
	limits    Limits
	admission *streamAdmission

	defaultID  Identity
	identities *identityRegistry

	tlsConf         *forktls.Config
	quicConf        *quicgo.Config
	connLimiter     *connectionLimiter
	connRateLimiter *newConnectionRateLimiter

	mu   sync.Mutex
	tr   *quicgo.Transport
	ln   *quicgo.Listener
	done chan struct{}

	started   bool
	ready     chan struct{}
	readyOnce sync.Once
	readyErr  error
	closeOnce sync.Once
	closeErr  error
}

// NewServer builds a Server hosting the given Ed25519 identities. The first key
// is the default identity used when a client sends no SNI; additional keys are
// reachable by their ADNL-id SNI.
func NewServer(handler Handler, keys ...ed25519.PrivateKey) (*Server, error) {
	return NewServerWithLimits(handler, DefaultLimits(), keys...)
}

// NewServerWithLimits builds a Server with explicit per-instance resource
// limits.
func NewServerWithLimits(handler Handler, limits Limits, keys ...ed25519.PrivateKey) (*Server, error) {
	if handler.OnQuery == nil {
		return nil, errors.New("quic: Handler.OnQuery is required")
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}

	identities, err := newIdentityRegistry(keys...)
	if err != nil {
		return nil, err
	}

	return newServer(handler, limits, identities, nil, nil), nil
}

func newServer(
	handler Handler,
	limits Limits,
	identities *identityRegistry,
	admission *streamAdmission,
	paths *Gateway,
) *Server {
	if admission == nil {
		admission = newStreamAdmission(
			limits.MaxConcurrentIncomingStreams,
			limits.MaxBufferedIncomingBytes,
		)
	}

	s := &Server{
		handler:         handler,
		paths:           paths,
		limits:          limits,
		admission:       admission,
		defaultID:       identities.defaultIdentity(),
		identities:      identities,
		quicConf:        defaultQUICConfig(limits),
		connLimiter:     newConnectionLimiter(limits.MaxConnections, limits.MaxConnectionsPerIP),
		connRateLimiter: newConnectionRateLimits(limits),
		done:            make(chan struct{}),
		ready:           make(chan struct{}),
	}
	s.tlsConf = s.buildTLSConfig()
	return s
}

// AddIdentity makes an Ed25519 identity available to new QUIC handshakes.
// Adding the same identity more than once is a no-op.
func (s *Server) AddIdentity(key ed25519.PrivateKey) error {
	_, err := s.identities.add(key)
	return err
}

// RemoveIdentity removes a non-default identity from new QUIC handshakes.
// Connections authenticated before removal remain active.
func (s *Server) RemoveIdentity(publicKey ed25519.PublicKey) error {
	id, err := idFromPublicKey(publicKey)
	if err != nil {
		return err
	}
	return s.identities.remove(id)
}

func (s *Server) buildTLSConfig() *forktls.Config {
	return &forktls.Config{
		MinVersion:             forktls.VersionTLS13,
		NextProtos:             []string{ALPN},
		ClientAuth:             forktls.RequireAnyClientCert, // mutual RPK
		SessionTicketsDisabled: true,                         // every conn re-verifies the peer key
		RawPublicKeys: &forktls.RawPublicKeyConfig{
			PrivateKey: s.defaultID.key,
			GetPrivateKey: func(sni string) (ed25519.PrivateKey, error) {
				if sni == "" {
					return nil, nil
				}
				id, err := parseSNI(sni)
				if err != nil {
					return nil, err
				}
				identity, err := s.identities.get(id)
				if err == nil {
					return identity.key, nil
				}
				return nil, fmt.Errorf("quic: unknown SNI identity %s", id)
			},
			// Accept any client key; the per-connection ADNL id is read from
			// the handshake state after completion.
			Verify: func(ed25519.PublicKey) error { return nil },
		},
	}
}

// Serve listens on pc and blocks serving connections until Close is called or a
// fatal accept error occurs.
func (s *Server) Serve(pc net.PacketConn) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("quic: server is already started")
	}
	select {
	case <-s.done:
		s.mu.Unlock()
		s.finishReady(ErrServerClosed)
		return ErrServerClosed
	default:
	}
	s.started = true
	s.mu.Unlock()

	tr := &quicgo.Transport{
		Conn: pc,
		VerifySourceAddress: func(net.Addr) bool {
			return true
		},
		ConnContext: s.connContext,
	}
	ln, err := tr.Listen(s.tlsConf, s.quicConf)
	if err != nil {
		tr.Close()
		err = fmt.Errorf("quic: listen: %w", err)
		s.finishReady(err)
		return err
	}

	s.mu.Lock()
	s.tr = tr
	s.ln = ln
	select {
	case <-s.done:
		s.mu.Unlock()
		_ = ln.Close()
		_ = tr.Close()
		s.finishReady(ErrServerClosed)
		return nil
	default:
	}
	s.mu.Unlock()
	s.finishReady(nil)

	for {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
				return fmt.Errorf("quic: accept: %w", err)
			}
		}
		go s.serveConn(conn)
	}
}

// WaitReady waits until the server listener is ready or startup fails.
func (s *Server) WaitReady(ctx context.Context) error {
	select {
	case <-s.ready:
		return s.readyErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) finishReady(err error) {
	s.readyOnce.Do(func() {
		s.readyErr = err
		close(s.ready)
	})
}

func (s *Server) connContext(ctx context.Context, info *quicgo.ClientInfo) (context.Context, error) {
	release, ok := s.connLimiter.acquire(info.RemoteAddr)
	if !ok {
		return nil, errTooManyConnections
	}
	if err := s.connRateLimiter.allow(info.RemoteAddr); err != nil {
		release()
		return nil, err
	}
	context.AfterFunc(ctx, release)
	return ctx, nil
}

func (s *Server) serveConn(conn *quicgo.Conn) {
	peerKey, err := peerPublicKey(conn)
	if err != nil {
		conn.CloseWithError(1, "unauthenticated peer")
		return
	}
	peer := adnlIDFromKey(peerKey)
	local, ok := s.localID(conn)
	if !ok {
		conn.CloseWithError(1, "malformed SNI")
		return
	}
	if !s.identities.has(local) {
		conn.CloseWithError(1, "local identity removed")
		return
	}
	ctx := conn.Context()

	var handler streamHandler
	if s.paths != nil {
		handler, err = s.paths.handlePath(ctx, local, peer, peerKey, conn)
		if err != nil {
			conn.CloseWithError(1, "path rejected")
			return
		}
	} else {
		handler = directStreamHandler{handler: s.handler, peerKey: peerKey}
	}

	serveConnStreams(ctx, conn, s.admission, s.limits, handler)
}

func (s *Server) localID(conn *quicgo.Conn) (adnlID, bool) {
	sni := conn.ConnectionState().TLS.ServerName
	if sni == "" {
		return s.defaultID.id, true
	}
	id, err := parseSNI(sni)
	if err != nil {
		// The same parseSNI already succeeded in GetPrivateKey during the
		// handshake; fail closed rather than serve as another identity.
		return adnlID{}, false
	}
	return id, true
}

// directStreamHandler dispatches admitted streams to a Server's Handler
// callbacks for one authenticated peer.
type directStreamHandler struct {
	handler Handler
	peerKey ed25519.PublicKey
}

func (h directStreamHandler) handleQuery(ctx context.Context, payload []byte) ([]byte, error) {
	return h.handler.OnQuery(ctx, h.peerKey, payload)
}

func (h directStreamHandler) handleMessage(ctx context.Context, payload []byte) {
	if h.handler.OnMessage != nil {
		h.handler.OnMessage(ctx, h.peerKey, payload)
	}
}

// The per-connection slot is taken BEFORE AcceptStream: quic-go returns
// MAX_STREAMS credit only when an accepted stream completes, so not accepting is
// QUIC-level backpressure, scoped to the one connection that is over budget.
//
// admission is the receiver-wide scope and is nil for connections we dialed:
// outbound work must never be gated on inbound pressure, the same rule as
// quic-server.cpp:653.
func serveConnStreams(ctx context.Context, conn *quicgo.Conn, admission *streamAdmission, limits Limits, handler streamHandler) {
	connection := newStreamAdmission(
		limits.MaxConcurrentIncomingStreamsPerConnection,
		limits.MaxBufferedIncomingBytesPerConnection,
	)
	guaranteed := limits.guaranteedStreamsPerConnection()

	for {
		if !connection.acquireSlot(ctx.Done()) {
			return
		}

		// A connection's first few concurrent streams never consult the shared pool,
		// so no set of peers can starve another down to zero throughput.
		lease := &streamAdmissionLease{connection: connection}
		if admission != nil && connection.activeStreams() > guaranteed {
			if !admission.acquireSlot(ctx.Done()) {
				connection.releaseSlot()
				return
			}
			lease.admission = admission
			lease.globalSlot = true
		} else if admission != nil {
			// Still charge bytes globally even when the slot was guaranteed.
			lease.admission = admission
		}

		st, err := conn.AcceptStream(ctx)
		if err != nil {
			lease.release()
			connection.releaseSlot()
			return // connection closed
		}

		go func() {
			defer connection.releaseSlot()
			serveAdmittedStream(ctx, st, lease, handler, limits)
		}()
	}
}

func serveAdmittedStream(
	ctx context.Context,
	st *quicgo.Stream,
	lease *streamAdmissionLease,
	handler streamHandler,
	limits Limits,
) {
	defer lease.release()

	id, payload, err := readIncomingBoxedObject(st, limits.MaxObjectSize, limits, lease)
	if err != nil {
		st.CancelRead(1)
		st.Close()
		return
	}

	switch id {
	case idQuicQuery:
		answer, herr := handler.handleQuery(st.Context(), payload)
		if herr != nil {
			st.CancelWrite(1)
			return
		}
		if err = writeAnswerWithIdleDeadline(st, answer, limits); err != nil {
			st.CancelWrite(1)
		}
	case idQuicMessage:
		handler.handleMessage(ctx, payload)
		// The C++ node replies with an empty, FIN-terminated stream.
		_ = st.Close()
	default:
		st.CancelRead(1)
		st.CancelWrite(1)
	}
}

// An idle deadline rather than one absolute budget: a peer that grants no
// flow-control credit is still cut off, a slow one that keeps draining is not.
func writeAnswerWithIdleDeadline(st *quicgo.Stream, answer []byte, limits Limits) error {
	w := &idleDeadlineWriter{
		st: st,
		d: newIdleDeadline(
			st.SetWriteDeadline,
			limits.StreamWriteTimeout(),
			limits.StreamTotalTimeout,
		),
	}
	defer func() { _ = st.SetWriteDeadline(time.Time{}) }()
	return writeBoxedObjectVia(w, st, idQuicAnswer, answer)
}

// Close stops the server. The PacketConn passed to Serve remains owned by the
// caller and must be closed separately.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		close(s.done)
		ln := s.ln
		tr := s.tr
		s.mu.Unlock()

		s.finishReady(ErrServerClosed)

		if ln != nil {
			s.closeErr = ln.Close()
		}
		if tr != nil {
			if closeErr := tr.Close(); s.closeErr == nil {
				s.closeErr = closeErr
			}
		}
	})
	return s.closeErr
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client is an outbound TON QUIC connection to a single peer.
type Client struct {
	conn          *quicgo.Conn
	peerKey       ed25519.PublicKey
	peer          adnlID
	maxObjectSize int64
}

// Dial establishes a TON QUIC connection to addr ("host:port"), authenticating
// with localKey and requiring the server to present expectedPeer's raw public
// key. The context bounds the handshake.
func Dial(ctx context.Context, addr string, localKey ed25519.PrivateKey, expectedPeer ed25519.PublicKey) (*Client, error) {
	return DialWithLimits(ctx, addr, localKey, expectedPeer, DefaultLimits())
}

// DialWithLimits establishes a standalone TON QUIC connection with explicit
// per-instance limits.
func DialWithLimits(ctx context.Context, addr string, localKey ed25519.PrivateKey, expectedPeer ed25519.PublicKey, limits Limits) (*Client, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}

	local, err := NewIdentity(localKey)
	if err != nil {
		return nil, err
	}

	return dialIdentity(ctx, addr, local, expectedPeer, limits)
}

func dialIdentity(ctx context.Context, addr string, local Identity, expectedPeer ed25519.PublicKey, limits Limits) (*Client, error) {
	tlsConf, expectedPeerID, err := clientTLSConfig(local, expectedPeer)
	if err != nil {
		return nil, err
	}
	remoteAddr, err := parseDialEndpoint(addr)
	if err != nil {
		return nil, err
	}

	// Before DialAddr opens its private UDP socket: passing the numeric endpoint
	// keeps the fork's resolve-error path from leaking it.
	conn, err := quicgo.DialAddr(ctx, remoteAddr.String(), tlsConf, defaultQUICConfig(limits))
	if err != nil {
		return nil, fmt.Errorf("quic: dial %s: %w", addr, err)
	}

	return authenticatedClient(conn, expectedPeerID, limits.MaxObjectSize)
}

func (s *Server) dialIdentity(ctx context.Context, addr string, local Identity, expectedPeer ed25519.PublicKey) (*Client, error) {
	if err := s.WaitReady(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	tr := s.tr
	s.mu.Unlock()
	if tr == nil {
		return nil, ErrServerClosed
	}

	return dialOn(ctx, tr, s.quicConf, addr, local, expectedPeer, s.limits.MaxObjectSize)
}

// The handshake shared by the server and client transports, which differ only in
// which quic-go transport and config they own.
func dialOn(
	ctx context.Context,
	tr *quicgo.Transport,
	conf *quicgo.Config,
	addr string,
	local Identity,
	expectedPeer ed25519.PublicKey,
	maxObjectSize int64,
) (*Client, error) {
	remoteAddr, err := parseDialEndpoint(addr)
	if err != nil {
		return nil, err
	}
	tlsConf, expectedPeerID, err := clientTLSConfig(local, expectedPeer)
	if err != nil {
		return nil, err
	}

	conn, err := tr.Dial(ctx, remoteAddr, tlsConf, conf)
	if err != nil {
		return nil, fmt.Errorf("quic: dial %s: %w", addr, err)
	}
	return authenticatedClient(conn, expectedPeerID, maxObjectSize)
}

func clientTLSConfig(local Identity, expectedPeer ed25519.PublicKey) (*forktls.Config, adnlID, error) {
	expectedPeerID, err := idFromPublicKey(expectedPeer)
	if err != nil {
		return nil, adnlID{}, err
	}
	expectedKey := append(ed25519.PublicKey(nil), expectedPeer...)

	tlsConf := &forktls.Config{
		MinVersion:             forktls.VersionTLS13,
		NextProtos:             []string{ALPN},
		ServerName:             expectedPeerID.sni(),
		SessionTicketsDisabled: true, // every conn re-verifies the peer key
		RawPublicKeys: &forktls.RawPublicKeyConfig{
			PrivateKey: local.key,
			Verify: func(peer ed25519.PublicKey) error {
				if !bytes.Equal(peer, expectedKey) {
					return fmt.Errorf("quic: server key %s does not match expected %s",
						adnlIDFromKey(peer), expectedPeerID)
				}
				return nil
			},
		},
	}

	return tlsConf, expectedPeerID, nil
}

func authenticatedClient(conn *quicgo.Conn, expectedPeerID adnlID, maxObjectSize int64) (*Client, error) {
	peerKey, err := peerPublicKey(conn)
	if err != nil {
		conn.CloseWithError(1, "peer id")
		return nil, err
	}
	peer := adnlIDFromKey(peerKey)
	if peer != expectedPeerID {
		conn.CloseWithError(1, "peer mismatch")
		return nil, fmt.Errorf("quic: connected peer %s != expected %s", peer, expectedPeerID)
	}

	return &Client{conn: conn, peerKey: peerKey, peer: peer, maxObjectSize: maxObjectSize}, nil
}

// PeerID returns a copy of the authenticated ADNL id of the remote endpoint.
func (c *Client) PeerID() []byte { return c.peer.bytes() }

// PeerKey returns a copy of the authenticated Ed25519 public key of the remote endpoint.
func (c *Client) PeerKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), c.peerKey...)
}

// Query sends a quic.query and returns the quic.answer payload. A positive
// maxAnswer is the whole boxed response stream cap and may exceed the default
// object size.
func (c *Client) Query(ctx context.Context, payload []byte, maxAnswer int64) ([]byte, error) {
	if err := validateBoxedObjectSize(payload, c.maxObjectSize); err != nil {
		return nil, fmt.Errorf("quic: query: %w", err)
	}

	// OpenStreamSync, not OpenStream: with the peer's MAX_STREAMS credit
	// momentarily exhausted OpenStream turns ordinary backpressure into a hard
	// error. The peer returns credit as it serves streams, and ctx bounds the wait.
	st, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("quic: open stream: %w", err)
	}
	stopCancel := cancelStreamOnContext(ctx, st)
	defer stopCancel()

	if dl, ok := ctx.Deadline(); ok {
		_ = st.SetDeadline(dl)
	}

	if err := writeBoxedObject(st, idQuicQuery, payload); err != nil {
		st.CancelRead(1)
		return nil, ctxErrOr(ctx, "write query", err)
	}

	limit := maxAnswer
	if limit <= 0 {
		limit = c.maxObjectSize
	}
	id, ansPayload, err := readBoxedObject(st, limit)
	if err != nil {
		st.CancelRead(1)
		return nil, ctxErrOr(ctx, "read answer", err)
	}
	if id != idQuicAnswer {
		st.CancelRead(1)
		return nil, fmt.Errorf("quic: expected quic.answer, got id 0x%08x", id)
	}
	return ansPayload, nil
}

// SendMessage sends a fire-and-forget quic.message.
func (c *Client) SendMessage(ctx context.Context, payload []byte) error {
	return c.sendMessageParts(ctx, nil, payload)
}

// SendMessageParts sends one fire-and-forget quic.message from two immutable
// payload segments. It avoids joining a stable overlay prefix with a prepared
// broadcast body for every recipient.
func (c *Client) SendMessageParts(ctx context.Context, prefix, body []byte) error {
	return c.sendMessageParts(ctx, prefix, body)
}

func (c *Client) sendMessageParts(ctx context.Context, prefix, body []byte) error {
	payloadLen := len(prefix) + len(body)
	if payloadLen < len(prefix) {
		return fmt.Errorf("quic: message: payload size overflow")
	}
	if err := validateBoxedObjectPayloadSize(payloadLen, c.maxObjectSize); err != nil {
		return fmt.Errorf("quic: message: %w", err)
	}

	// See Query: waiting for stream credit under ctx beats failing the send.
	st, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("quic: open stream: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = st.SetDeadline(dl)
	} else {
		stopCancel := cancelStreamOnContext(ctx, st)
		defer stopCancel()
	}
	if err := writeBoxedObjectParts(st, idQuicMessage, prefix, body); err != nil {
		st.CancelRead(1)
		return ctxErrOr(ctx, "write message", err)
	}
	// Fire-and-forget: the peer sends back an empty FIN-terminated stream,
	// which we don't need.
	st.CancelRead(1)
	return nil
}

// Prefers the context's own error: a cancelled call should report why it was cut
// short, not the I/O failure that cancelling caused.
func ctxErrOr(ctx context.Context, what string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return fmt.Errorf("quic: %s: %w", what, err)
}

func cancelStreamOnContext(ctx context.Context, st *quicgo.Stream) func() bool {
	return context.AfterFunc(ctx, func() {
		st.CancelRead(1)
		st.CancelWrite(1)
	})
}

// Close tears down the connection.
func (c *Client) Close() error {
	return c.conn.CloseWithError(0, "")
}

// ---------------------------------------------------------------------------
// Stream helpers
// ---------------------------------------------------------------------------

// Recycled only after a fully successful write: after an error the fork's
// SendStream may still reference the slice, so it must be left to the GC.
var wireBuffers = sync.Pool{
	New: func() any {
		buf := make([]byte, directWriteObjectThreshold+16)
		return &buf
	},
}

// writeBoxedObject writes a boxed <id> data:bytes object and closes the stream's send side (FIN).
func writeBoxedObject(st *quicgo.Stream, id uint32, payload []byte) error {
	return writeBoxedObjectPartsVia(st, st, id, nil, payload)
}

func writeBoxedObjectParts(st *quicgo.Stream, id uint32, prefix, body []byte) error {
	return writeBoxedObjectPartsVia(st, st, id, prefix, body)
}

// Split from writeBoxedObject so the answer path can wrap the stream in an
// idle-deadline writer while small objects keep the pooled single-write path.
func writeBoxedObjectVia(w io.Writer, st *quicgo.Stream, id uint32, payload []byte) error {
	return writeBoxedObjectPartsVia(w, st, id, nil, payload)
}

func writeBoxedObjectPartsVia(w io.Writer, st *quicgo.Stream, id uint32, prefix, body []byte) error {
	payloadLen := len(prefix) + len(body)
	if payloadLen < len(prefix) {
		return fmt.Errorf("payload size overflow")
	}
	if payloadLen < directWriteObjectThreshold {
		header, headerLen, pad, total, err := boxedObjectHeader(id, payloadLen)
		if err != nil {
			return err
		}
		buf := wireBuffers.Get().(*[]byte)
		wire := (*buf)[:total]
		copy(wire, header[:headerLen])
		offset := headerLen + copy(wire[headerLen:], prefix)
		copy(wire[offset:], body)
		clear(wire[total-pad:])
		err = writeFull(w, wire)
		wireBuffers.Put(buf)
		if err != nil {
			return err
		}
		return st.Close()
	}

	if err := writeBoxedObjectPartsTo(w, id, prefix, body); err != nil {
		return err
	}
	return st.Close()
}

func validateBoxedObjectSize(payload []byte, maxSize int64) error {
	return validateBoxedObjectPayloadSize(len(payload), maxSize)
}

func validateBoxedObjectPayloadSize(payloadLen int, maxSize int64) error {
	_, _, _, total, err := boxedObjectHeader(0, payloadLen)
	if err != nil {
		return err
	}
	if maxSize <= 0 || int64(total) > maxSize {
		return fmt.Errorf("quic: stream object exceeds %d bytes", maxSize)
	}
	return nil
}

func writeBoxedObjectTo(w io.Writer, id uint32, payload []byte) error {
	return writeBoxedObjectPartsTo(w, id, nil, payload)
}

func writeBoxedObjectPartsTo(w io.Writer, id uint32, prefix, body []byte) error {
	payloadLen := len(prefix) + len(body)
	if payloadLen < len(prefix) {
		return fmt.Errorf("payload size overflow")
	}
	header, headerLen, pad, _, err := boxedObjectHeader(id, payloadLen)
	if err != nil {
		return err
	}
	if err = writeFull(w, header[:headerLen]); err != nil {
		return err
	}
	if len(prefix) > 0 {
		if err = writeFull(w, prefix); err != nil {
			return err
		}
	}
	if err = writeFull(w, body); err != nil {
		return err
	}
	if pad > 0 {
		var zeros [3]byte
		if err = writeFull(w, zeros[:pad]); err != nil {
			return err
		}
	}
	return nil
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func readBoxedObject(r io.Reader, maxSize int64) (uint32, []byte, error) {
	return readBoxedObjectAdmitted(r, maxSize, nil)
}

// How much payload is charged and read at a time before the stream has proven
// itself, bounding what a peer gets for free by declaring a huge length.
const payloadReadChunk = 64 << 10

// Past this a stream is treated as real and the rest is allocated in one go, so a
// legitimate multi-MiB broadcast does not pay repeated regrow copies.
const payloadCommitThreshold = 1 << 20

// Charges the admission scopes for bytes that actually arrived rather than for
// the declared length, so a peer cannot pin MaxObjectSize of budget and heap per
// stream by sending only the header. Mirrors QuicSender::StreamState::append.
func readAdmittedPayload(r io.Reader, payloadLen int, lease *streamAdmissionLease) ([]byte, error) {
	if payloadLen == 0 {
		return nil, nil
	}

	// Nothing is allocated before the first chunk is charged.
	var payload []byte
	for len(payload) < payloadLen {
		want := payloadLen - len(payload)
		if len(payload) < payloadCommitThreshold && want > payloadReadChunk {
			want = payloadReadChunk
		}
		if lease != nil {
			if err := lease.chargeBytes(int64(want)); err != nil {
				return nil, err
			}
		}

		if cap(payload)-len(payload) < want {
			// Geometric until the stream proves itself, then one exact resize.
			capacity := len(payload) + want
			if doubled := 2 * cap(payload); doubled > capacity {
				capacity = doubled
			}
			if capacity > payloadLen {
				capacity = payloadLen
			}
			grown := make([]byte, len(payload), capacity)
			copy(grown, payload)
			payload = grown
		}

		start := len(payload)
		payload = payload[:start+want]
		if _, err := io.ReadFull(r, payload[start:]); err != nil {
			return nil, err
		}
	}
	return payload, nil
}

// Bounds a stream by how long it stays silent rather than by one absolute budget,
// so a slow but progressing peer is never cut off while one that stops entirely
// still is. total, when set, is the outer bound an idle window may not push past.
type idleDeadline struct {
	set  func(time.Time) error
	idle time.Duration

	deadline time.Time
	hardStop time.Time
}

func newIdleDeadline(set func(time.Time) error, idle, total time.Duration) *idleDeadline {
	d := &idleDeadline{set: set, idle: idle}
	now := time.Now()
	if total > 0 {
		d.hardStop = now.Add(total)
	}
	d.arm(now)
	return d
}

func (d *idleDeadline) arm(now time.Time) {
	deadline := now.Add(d.idle)
	if !d.hardStop.IsZero() && deadline.After(d.hardStop) {
		deadline = d.hardStop
	}
	d.deadline = deadline
	_ = d.set(deadline)
}

// Re-arms only once the window is more than half spent, so a large object does
// not touch the deadline setter on every chunk.
func (d *idleDeadline) progressed() {
	if now := time.Now(); d.deadline.Sub(now) < d.idle/2 {
		d.arm(now)
	}
}

type idleDeadlineWriter struct {
	st *quicgo.Stream
	d  *idleDeadline
}

func (w *idleDeadlineWriter) Write(p []byte) (int, error) {
	n, err := w.st.Write(p)
	if n > 0 {
		w.d.progressed()
	}
	return n, err
}

type idleDeadlineReader struct {
	st *quicgo.Stream
	d  *idleDeadline
}

func (r *idleDeadlineReader) Read(p []byte) (int, error) {
	n, err := r.st.Read(p)
	if n > 0 {
		r.d.progressed()
	}
	return n, err
}

// Reads one object under an idle read deadline. An absolute budget would be an
// implicit minimum bandwidth instead: at StreamReadTimeout=15s a 16 MiB broadcast
// needed a sustained ~1.1 MB/s or it was killed mid-progress. The reference node
// puts no timeout on inbound streams at all (quic-server.h), so an idle bound is
// still stricter than C++.
func readIncomingBoxedObject(st *quicgo.Stream, maxSize int64, limits Limits, lease *streamAdmissionLease) (uint32, []byte, error) {
	r := &idleDeadlineReader{
		st: st,
		d: newIdleDeadline(
			st.SetReadDeadline,
			limits.StreamReadTimeout,
			limits.StreamTotalTimeout,
		),
	}
	id, payload, err := readBoxedObjectAdmitted(r, maxSize, lease)
	if err == nil {
		_ = st.SetReadDeadline(time.Time{})
	}
	return id, payload, err
}

func readBoxedObjectAdmitted(r io.Reader, maxSize int64, lease *streamAdmissionLease) (uint32, []byte, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:5]); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return 0, nil, err
	}

	id := binary.LittleEndian.Uint32(header[:4])
	bytesHeaderLen := uint64(1)
	payloadLen := uint64(header[4])
	switch header[4] {
	case 0xFE:
		if _, err := io.ReadFull(r, header[5:8]); err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return 0, nil, err
		}
		bytesHeaderLen = 4
		payloadLen = uint64(header[5]) | uint64(header[6])<<8 | uint64(header[7])<<16
	case 0xFF:
		if _, err := io.ReadFull(r, header[5:12]); err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return 0, nil, err
		}
		if header[9] != 0 || header[10] != 0 || header[11] != 0 {
			return 0, nil, errors.New("quic: extended TL bytes length exceeds uint32")
		}
		bytesHeaderLen = 8
		payloadLen = uint64(binary.LittleEndian.Uint32(header[5:9]))
	}

	bytesLen := bytesHeaderLen + payloadLen
	var pad uint64
	if rem := bytesLen % 4; rem != 0 {
		pad = 4 - rem
	}
	total := uint64(4) + bytesLen + pad
	if maxSize <= 0 || total > uint64(maxSize) {
		return 0, nil, fmt.Errorf("quic: stream object exceeds %d bytes", maxSize)
	}

	maxInt := uint64(^uint(0) >> 1)
	if payloadLen > maxInt {
		return 0, nil, fmt.Errorf("quic: payload size %d overflows int", payloadLen)
	}

	payload, err := readAdmittedPayload(r, int(payloadLen), lease)
	if err != nil {
		return 0, nil, err
	}
	if pad > 0 {
		// The padding must arrive, but its content is not inspected - matching
		// tl.fromBytes and td::TlParser::fetch_string.
		var padding [3]byte
		if _, err := io.ReadFull(r, padding[:int(pad)]); err != nil {
			return 0, nil, err
		}
	}

	var extra [1]byte
	n, err := r.Read(extra[:])
	if n > 0 {
		return 0, nil, errors.New("quic: trailing bytes after boxed object")
	}
	if err == nil {
		return 0, nil, io.ErrNoProgress
	}
	if !errors.Is(err, io.EOF) {
		return 0, nil, err
	}
	return id, payload, nil
}

// peerPublicKey extracts the peer's Ed25519 public key from the RPK-authenticated TLS state.
func peerPublicKey(conn *quicgo.Conn) (ed25519.PublicKey, error) {
	pub := conn.ConnectionState().TLS.PeerRawPublicKey
	if len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("quic: peer did not present a raw public key")
	}
	return append(ed25519.PublicKey(nil), pub...), nil
}
