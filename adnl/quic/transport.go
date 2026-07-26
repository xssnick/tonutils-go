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
	defaultInitialStreamReceiveWindow     = 256 << 10
	defaultMaxStreamReceiveWindow         = 6 << 20
	defaultInitialConnectionReceiveWindow = 4 << 20
	defaultMaxConnectionReceiveWindow     = 24 << 20
	defaultMaxConnectionsPerIP            = 1000
	directWriteObjectThreshold            = 32 << 10
)

// Handler processes inbound stream objects for a Server.
type Handler struct {
	// OnQuery handles a quic.query and returns the answer payload. Required.
	OnQuery func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error)
	// OnMessage handles a fire-and-forget quic.message. Optional.
	OnMessage func(ctx context.Context, from ed25519.PublicKey, payload []byte)

	pathHandler serverPathHandler
}

type streamPath struct {
	local   adnlID
	peer    adnlID
	peerKey ed25519.PublicKey
	conn    *quicgo.Conn
}

type streamHandler interface {
	handleQuery(ctx context.Context, payload []byte) ([]byte, error)
	handleMessage(ctx context.Context, payload []byte)
}

type serverPathHandler interface {
	handlePath(ctx context.Context, path streamPath) (streamHandler, error)
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
	handler       Handler
	maxObjectSize int64
	limits        Limits
	admission     *streamAdmission

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
	if handler.OnQuery == nil && handler.pathHandler == nil {
		return nil, errors.New("quic: Handler.OnQuery is required")
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}

	identities, err := newIdentityRegistry(keys...)
	if err != nil {
		return nil, err
	}

	return newServer(handler, limits, identities, nil), nil
}

func newServer(handler Handler, limits Limits, identities *identityRegistry, admission *streamAdmission) *Server {
	if admission == nil {
		admission = newStreamAdmission(
			uint64(limits.MaxConcurrentIncomingStreams),
			uint64(limits.MaxBufferedIncomingBytes),
		)
	}

	s := &Server{
		handler:         handler,
		maxObjectSize:   limits.MaxObjectSize,
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
	if s.handler.pathHandler != nil {
		handler, err = s.handler.pathHandler.handlePath(ctx, streamPath{
			local:   local,
			peer:    peer,
			peerKey: peerKey,
			conn:    conn,
		})
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

// serveConnStreams accepts inbound streams on conn and serves each one under
// a stream admission lease. When either admission scope is exhausted it stops
// accepting further streams until a lease is released, so quic-go's stream
// limits and flow control backpressure the peer.
func serveConnStreams(ctx context.Context, conn *quicgo.Conn, admission *streamAdmission, limits Limits, handler streamHandler) {
	connection := newStreamAdmission(
		uint64(limits.MaxConcurrentIncomingStreamsPerConnection),
		uint64(limits.MaxBufferedIncomingBytesPerConnection),
	)
	stopWake := context.AfterFunc(ctx, func() {
		admission.wake()
		connection.wake()
	})
	defer stopWake()

	for {
		st, err := conn.AcceptStream(ctx)
		if err != nil {
			return // connection closed
		}

		lease, err := admission.acquireStreamWithin(ctx, connection)
		if err != nil {
			return
		}
		go serveAdmittedStream(ctx, st, lease, handler, limits.MaxObjectSize, limits.StreamReadTimeout)
	}
}

func serveAdmittedStream(
	ctx context.Context,
	st *quicgo.Stream,
	lease streamAdmissionLease,
	handler streamHandler,
	maxObjectSize int64,
	readTimeout time.Duration,
) {
	defer lease.release()

	id, payload, err := readIncomingBoxedObject(st, maxObjectSize, readTimeout, &lease)
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
		// The answer write must not outlive the read bound: admission slots are
		// a backpressure resource now, and a peer withholding stream
		// flow-control credit while keeping the connection alive would pin this
		// slot forever otherwise.
		_ = st.SetWriteDeadline(time.Now().Add(readTimeout))
		if err = writeBoxedObject(st, idQuicAnswer, answer); err != nil {
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

	// Parse before DialAddr opens its private UDP socket. Besides avoiding
	// socket allocation for malformed addresses, passing the numeric endpoint
	// prevents the fork's resolve-error path from leaking that socket.
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

	remoteAddr, err := parseDialEndpoint(addr)
	if err != nil {
		return nil, err
	}
	tlsConf, expectedPeerID, err := clientTLSConfig(local, expectedPeer)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	tr := s.tr
	s.mu.Unlock()
	if tr == nil {
		return nil, ErrServerClosed
	}

	conn, err := tr.Dial(ctx, remoteAddr, tlsConf, s.quicConf)
	if err != nil {
		return nil, fmt.Errorf("quic: dial %s: %w", addr, err)
	}

	return authenticatedClient(conn, expectedPeerID, s.maxObjectSize)
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

	return clientFromConn(conn, peerKey, peer, maxObjectSize), nil
}

func clientFromConn(conn *quicgo.Conn, peerKey ed25519.PublicKey, peer adnlID, maxObjectSize int64) *Client {
	return &Client{conn: conn, peerKey: peerKey, peer: peer, maxObjectSize: maxObjectSize}
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

	st, err := c.conn.OpenStream()
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
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("quic: write query: %w", err)
	}

	limit := maxAnswer
	if limit <= 0 {
		limit = c.maxObjectSize
	}
	id, ansPayload, err := readBoxedObject(st, limit)
	if err != nil {
		st.CancelRead(1)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("quic: read answer: %w", err)
	}
	if id != idQuicAnswer {
		st.CancelRead(1)
		return nil, fmt.Errorf("quic: expected quic.answer, got id 0x%08x", id)
	}
	return ansPayload, nil
}

// SendMessage sends a fire-and-forget quic.message.
func (c *Client) SendMessage(ctx context.Context, payload []byte) error {
	if err := validateBoxedObjectSize(payload, c.maxObjectSize); err != nil {
		return fmt.Errorf("quic: message: %w", err)
	}

	st, err := c.conn.OpenStream()
	if err != nil {
		return fmt.Errorf("quic: open stream: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = st.SetDeadline(dl)
	} else {
		stopCancel := cancelStreamOnContext(ctx, st)
		defer stopCancel()
	}
	if err := writeBoxedObject(st, idQuicMessage, payload); err != nil {
		st.CancelRead(1)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("quic: write message: %w", err)
	}
	// Fire-and-forget: the peer sends back an empty FIN-terminated stream,
	// which we don't need.
	st.CancelRead(1)
	return nil
}

func cancelStreamOnContext(ctx context.Context, st *quicgo.Stream) func() {
	stop := context.AfterFunc(ctx, func() {
		st.CancelRead(1)
		st.CancelWrite(1)
	})
	return func() {
		stop()
	}
}

// Close tears down the connection.
func (c *Client) Close() error {
	return c.conn.CloseWithError(0, "")
}

// ---------------------------------------------------------------------------
// Stream helpers
// ---------------------------------------------------------------------------

// wireBuffers pools frame buffers for sub-threshold writeBoxedObject calls.
// A buffer is recycled only after a fully successful write: after a write
// error the fork's SendStream may still reference the slice (dataForWriting,
// reliable reset), so it must be left to the GC.
var wireBuffers = sync.Pool{
	New: func() any {
		buf := make([]byte, directWriteObjectThreshold+16)
		return &buf
	},
}

// writeBoxedObject writes a boxed <id> data:bytes object and closes the stream's send side (FIN).
func writeBoxedObject(st *quicgo.Stream, id uint32, payload []byte) error {
	if len(payload) < directWriteObjectThreshold {
		header, headerLen, pad, total, err := boxedObjectHeader(id, len(payload))
		if err != nil {
			return err
		}
		buf := wireBuffers.Get().(*[]byte)
		wire := (*buf)[:total]
		copy(wire, header[:headerLen])
		copy(wire[headerLen:], payload)
		clear(wire[total-pad:])
		if err = writeFull(st, wire); err != nil {
			return err
		}
		wireBuffers.Put(buf)
		return st.Close()
	}

	if err := writeBoxedObjectTo(st, id, payload); err != nil {
		return err
	}
	return st.Close()
}

func validateBoxedObjectSize(payload []byte, maxSize int64) error {
	_, _, _, total, err := boxedObjectHeader(0, len(payload))
	if err != nil {
		return err
	}
	if maxSize <= 0 || int64(total) > maxSize {
		return fmt.Errorf("quic: stream object exceeds %d bytes", maxSize)
	}
	return nil
}

func writeBoxedObjectTo(w io.Writer, id uint32, payload []byte) error {
	header, headerLen, pad, _, err := boxedObjectHeader(id, len(payload))
	if err != nil {
		return err
	}
	if err = writeFull(w, header[:headerLen]); err != nil {
		return err
	}
	if err = writeFull(w, payload); err != nil {
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

func readIncomingBoxedObject(st *quicgo.Stream, maxSize int64, timeout time.Duration, lease *streamAdmissionLease) (uint32, []byte, error) {
	_ = st.SetReadDeadline(time.Now().Add(timeout))
	id, payload, err := readBoxedObjectAdmitted(st, maxSize, lease)
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

	if lease != nil {
		if err := lease.reservePayload(payloadLen); err != nil {
			return 0, nil, err
		}
	}

	payload := make([]byte, int(payloadLen))
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if pad > 0 {
		var padding [3]byte
		if _, err := io.ReadFull(r, padding[:int(pad)]); err != nil {
			return 0, nil, err
		}
		for _, value := range padding[:int(pad)] {
			if value != 0 {
				return 0, nil, errors.New("quic: TL bytes alignment padding is not zero")
			}
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
