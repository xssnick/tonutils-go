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
	"sync"
	"time"

	quicgo "github.com/xssnick/quic-go-ton"
	forktls "github.com/xssnick/quic-go-ton/tls"
)

// ALPN is the application protocol negotiated for TON QUIC.
const ALPN = "ton"

// DefaultMaxObjectSize caps how many bytes a single stream object may carry.
// The value follows the C++ node's initial local bidi stream receive window.
const DefaultMaxObjectSize = 4 << 20

// MaxIncomingStreams limits concurrent incoming bidirectional streams.
// Set it before constructing clients or servers.
var MaxIncomingStreams int64 = 2048

const (
	defaultInitialStreamReceiveWindow     = 4 << 20
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

var errTooManyConnections = errors.New("quic: too many active connections from remote address")

type connectionLimiter struct {
	mu     sync.Mutex
	max    int
	active map[string]int
}

func newConnectionLimiter(max int) *connectionLimiter {
	return &connectionLimiter{
		max:    max,
		active: make(map[string]int),
	}
}

func (l *connectionLimiter) acquire(addr net.Addr) (func(), bool) {
	if l == nil || l.max <= 0 {
		return func() {}, true
	}

	key := remoteIPKey(addr)

	l.mu.Lock()
	if l.active[key] >= l.max {
		l.mu.Unlock()
		return nil, false
	}
	l.active[key]++
	l.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			if l.active[key] <= 1 {
				delete(l.active, key)
			} else {
				l.active[key]--
			}
			l.mu.Unlock()
		})
	}, true
}

func remoteIPKey(addr net.Addr) string {
	if udp, ok := addr.(*net.UDPAddr); ok {
		return udp.IP.String()
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err == nil {
		return host
	}
	return addr.String()
}

// defaultQUICConfig returns the default quic-go settings for TON connections.
func defaultQUICConfig() *quicgo.Config {
	return &quicgo.Config{
		Versions:                       []quicgo.Version{quicgo.Version1},
		MaxIdleTimeout:                 15 * time.Second,
		KeepAlivePeriod:                5 * time.Second,
		InitialStreamReceiveWindow:     defaultInitialStreamReceiveWindow,
		MaxStreamReceiveWindow:         defaultMaxStreamReceiveWindow,
		InitialConnectionReceiveWindow: defaultInitialConnectionReceiveWindow,
		MaxConnectionReceiveWindow:     defaultMaxConnectionReceiveWindow,
		MaxIncomingStreams:             MaxIncomingStreams,
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

	defaultID Identity
	byID      map[adnlID]Identity

	tlsConf     *forktls.Config
	quicConf    *quicgo.Config
	connLimiter *connectionLimiter

	mu   sync.Mutex
	tr   *quicgo.Transport
	ln   *quicgo.Listener
	done chan struct{}
}

// NewServer builds a Server hosting the given Ed25519 identities. The first key
// is the default identity used when a client sends no SNI; additional keys are
// reachable by their ADNL-id SNI.
func NewServer(handler Handler, keys ...ed25519.PrivateKey) (*Server, error) {
	if handler.OnQuery == nil && handler.pathHandler == nil {
		return nil, errors.New("quic: Handler.OnQuery is required")
	}
	if len(keys) == 0 {
		return nil, errors.New("quic: at least one identity key is required")
	}
	s := &Server{
		handler:       handler,
		maxObjectSize: DefaultMaxObjectSize,
		byID:          make(map[adnlID]Identity, len(keys)),
		quicConf:      defaultQUICConfig(),
		connLimiter:   newConnectionLimiter(defaultMaxConnectionsPerIP),
		done:          make(chan struct{}),
	}
	for i, k := range keys {
		id, err := NewIdentity(k)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			s.defaultID = id
		}
		s.byID[id.id] = id
	}
	s.tlsConf = s.buildTLSConfig()
	return s, nil
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
				if idn, ok := s.byID[id]; ok {
					return idn.key, nil
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
		return fmt.Errorf("quic: listen: %w", err)
	}
	s.mu.Lock()
	s.tr = tr
	s.ln = ln
	s.mu.Unlock()

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

func (s *Server) connContext(ctx context.Context, info *quicgo.ClientInfo) (context.Context, error) {
	release, ok := s.connLimiter.acquire(info.RemoteAddr)
	if !ok {
		return nil, errTooManyConnections
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
	local := s.localID(conn)
	ctx := conn.Context()

	var pathHandler streamHandler
	if s.handler.pathHandler != nil {
		pathHandler, err = s.handler.pathHandler.handlePath(ctx, streamPath{
			local:   local,
			peer:    peer,
			peerKey: peerKey,
			conn:    conn,
		})
		if err != nil {
			conn.CloseWithError(1, "path rejected")
			return
		}
	}

	for {
		st, err := conn.AcceptStream(ctx)
		if err != nil {
			return // connection closed
		}
		go s.serveStream(ctx, pathHandler, peerKey, st)
	}
}

func (s *Server) localID(conn *quicgo.Conn) adnlID {
	sni := conn.ConnectionState().TLS.ServerName
	if sni == "" {
		return s.defaultID.id
	}
	id, err := parseSNI(sni)
	if err != nil {
		return s.defaultID.id
	}
	return id
}

func (s *Server) serveStream(ctx context.Context, pathHandler streamHandler, peerKey ed25519.PublicKey, st *quicgo.Stream) {
	id, payload, err := readBoxedObject(st, s.maxObjectSize)
	if err != nil {
		st.CancelRead(1)
		st.Close()
		return
	}

	switch id {
	case idQuicQuery:
		var answer []byte
		var herr error
		if pathHandler != nil {
			answer, herr = pathHandler.handleQuery(ctx, payload)
		} else {
			answer, herr = s.handler.OnQuery(ctx, peerKey, payload)
		}
		if herr != nil {
			st.CancelWrite(1)
			return
		}
		if err := writeBoxedObject(st, idQuicAnswer, answer); err != nil {
			st.CancelWrite(1)
		}
	case idQuicMessage:
		if pathHandler != nil {
			pathHandler.handleMessage(ctx, payload)
		} else if s.handler.OnMessage != nil {
			s.handler.OnMessage(ctx, peerKey, payload)
		}
		// The C++ node replies with an empty, FIN-terminated stream.
		_ = st.Close()
	default:
		st.CancelRead(1)
		st.CancelWrite(1)
	}
}

// Close stops the server and releases its socket.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	var err error
	if s.ln != nil {
		err = s.ln.Close()
	}
	if s.tr != nil {
		s.tr.Close()
	}
	return err
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
	local, err := NewIdentity(localKey)
	if err != nil {
		return nil, err
	}

	return dialIdentity(ctx, addr, local, expectedPeer)
}

func dialIdentity(ctx context.Context, addr string, local Identity, expectedPeer ed25519.PublicKey) (*Client, error) {
	expectedPeerID, err := idFromPublicKey(expectedPeer)
	if err != nil {
		return nil, err
	}

	tlsConf := &forktls.Config{
		MinVersion:             forktls.VersionTLS13,
		NextProtos:             []string{ALPN},
		ServerName:             expectedPeerID.sni(),
		SessionTicketsDisabled: true, // every conn re-verifies the peer key
		RawPublicKeys: &forktls.RawPublicKeyConfig{
			PrivateKey: local.key,
			Verify: func(peer ed25519.PublicKey) error {
				if !bytes.Equal(peer, expectedPeer) {
					return fmt.Errorf("quic: server key %s does not match expected %s",
						adnlIDFromKey(peer), expectedPeerID)
				}
				return nil
			},
		},
	}

	conn, err := quicgo.DialAddr(ctx, addr, tlsConf, defaultQUICConfig())
	if err != nil {
		return nil, fmt.Errorf("quic: dial %s: %w", addr, err)
	}

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

	return clientFromConn(conn, peerKey, peer, DefaultMaxObjectSize), nil
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

// Query sends a quic.query and returns the quic.answer payload. maxAnswer, when
// positive, overrides the default answer size cap.
func (c *Client) Query(ctx context.Context, payload []byte, maxAnswer int64) ([]byte, error) {
	st, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("quic: open stream: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = st.SetDeadline(dl)
	}

	if err := writeBoxedObject(st, idQuicQuery, payload); err != nil {
		st.CancelRead(1)
		return nil, fmt.Errorf("quic: write query: %w", err)
	}

	limit := maxAnswer
	if limit <= 0 {
		limit = c.maxObjectSize
	}
	id, ansPayload, err := readBoxedObject(st, limit)
	if err != nil {
		st.CancelRead(1)
		return nil, fmt.Errorf("quic: read answer: %w", err)
	}
	if id != idQuicAnswer {
		return nil, fmt.Errorf("quic: expected quic.answer, got id 0x%08x", id)
	}
	return ansPayload, nil
}

// SendMessage sends a fire-and-forget quic.message.
func (c *Client) SendMessage(ctx context.Context, payload []byte) error {
	st, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("quic: open stream: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = st.SetDeadline(dl)
	}
	if err := writeBoxedObject(st, idQuicMessage, payload); err != nil {
		st.CancelRead(1)
		return fmt.Errorf("quic: write message: %w", err)
	}
	// Fire-and-forget: the peer sends back an empty FIN-terminated stream,
	// which we don't need.
	st.CancelRead(1)
	return nil
}

// Close tears down the connection.
func (c *Client) Close() error {
	return c.conn.CloseWithError(0, "")
}

// ---------------------------------------------------------------------------
// Stream helpers
// ---------------------------------------------------------------------------

// writeBoxedObject writes a boxed <id> data:bytes object and closes the stream's send side (FIN).
func writeBoxedObject(st *quicgo.Stream, id uint32, payload []byte) error {
	if len(payload) < directWriteObjectThreshold {
		wire, err := serializeBoxed(id, payload)
		if err != nil {
			return err
		}
		if err = writeFull(st, wire); err != nil {
			return err
		}
		return st.Close()
	}

	if err := writeBoxedObjectTo(st, id, payload); err != nil {
		return err
	}
	return st.Close()
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
	var header [8]byte
	if _, err := io.ReadFull(r, header[:5]); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return 0, nil, err
	}

	id := binary.LittleEndian.Uint32(header[:4])
	bytesHeaderLen := 1
	payloadLen := int(header[4])
	if payloadLen == 0xFE {
		if _, err := io.ReadFull(r, header[5:8]); err != nil {
			return 0, nil, err
		}
		bytesHeaderLen = 4
		payloadLen = int(header[5]) | int(header[6])<<8 | int(header[7])<<16
	}

	bytesLen := bytesHeaderLen + payloadLen
	pad := 0
	if rem := bytesLen % 4; rem != 0 {
		pad = 4 - rem
	}
	total := 4 + bytesLen + pad
	if int64(total) > maxSize {
		return 0, nil, fmt.Errorf("quic: stream object exceeds %d bytes", maxSize)
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if pad > 0 {
		var padding [3]byte
		if _, err := io.ReadFull(r, padding[:pad]); err != nil {
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
