package quic

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	quicgo "github.com/xssnick/quic-go-ton"
	forktls "github.com/xssnick/quic-go-ton/tls"
)

func mustKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

// startServer spins up a Server on a loopback UDP socket and returns its addr.
func startServer(t *testing.T, h Handler, keys ...ed25519.PrivateKey) (string, *Server) {
	return startServerWithConfig(t, h, nil, keys...)
}

func startServerWithConfig(t *testing.T, h Handler, configure func(*Server), keys ...ed25519.PrivateKey) (string, *Server) {
	t.Helper()
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(h, keys...)
	if err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(srv)
	}
	go func() { _ = srv.Serve(pc) }()
	t.Cleanup(func() { srv.Close() })
	return pc.LocalAddr().String(), srv
}

func TestDefaultLimitsMatchCppNode(t *testing.T) {
	if DefaultMaxObjectSize != 4<<20 {
		t.Fatalf("DefaultMaxObjectSize = %d, want %d", DefaultMaxObjectSize, 4<<20)
	}
	if defaultMaxConnectionsPerIP != 1000 {
		t.Fatalf("defaultMaxConnectionsPerIP = %d, want 1000", defaultMaxConnectionsPerIP)
	}

	cfg := defaultQUICConfig()
	if cfg.InitialStreamReceiveWindow != defaultInitialStreamReceiveWindow {
		t.Fatalf("InitialStreamReceiveWindow = %d, want %d", cfg.InitialStreamReceiveWindow, defaultInitialStreamReceiveWindow)
	}
	if cfg.MaxStreamReceiveWindow != defaultMaxStreamReceiveWindow {
		t.Fatalf("MaxStreamReceiveWindow = %d, want %d", cfg.MaxStreamReceiveWindow, defaultMaxStreamReceiveWindow)
	}
	if cfg.InitialConnectionReceiveWindow != defaultInitialConnectionReceiveWindow {
		t.Fatalf("InitialConnectionReceiveWindow = %d, want %d", cfg.InitialConnectionReceiveWindow, defaultInitialConnectionReceiveWindow)
	}
	if cfg.MaxConnectionReceiveWindow != defaultMaxConnectionReceiveWindow {
		t.Fatalf("MaxConnectionReceiveWindow = %d, want %d", cfg.MaxConnectionReceiveWindow, defaultMaxConnectionReceiveWindow)
	}
	if cfg.MaxIncomingStreams != defaultMaxIncomingStreams {
		t.Fatalf("MaxIncomingStreams = %d, want %d", cfg.MaxIncomingStreams, defaultMaxIncomingStreams)
	}
	if cfg.MaxIncomingUniStreams != -1 {
		t.Fatalf("MaxIncomingUniStreams = %d, want -1", cfg.MaxIncomingUniStreams)
	}
}

func TestConnectionLimiterRejectsPerIPOverflow(t *testing.T) {
	serverKey := mustKey(t)
	handler := Handler{OnQuery: func(context.Context, ed25519.PublicKey, []byte) ([]byte, error) { return nil, nil }}
	srv, err := NewServer(handler, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	srv.connLimiter.max = 1

	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err = srv.connContext(ctx, &quicgo.ClientInfo{RemoteAddr: addr}); err != nil {
		t.Fatalf("first connContext: %v", err)
	}
	if _, err = srv.connContext(context.Background(), &quicgo.ClientInfo{RemoteAddr: addr}); !errors.Is(err, errTooManyConnections) {
		t.Fatalf("second connContext err = %v, want %v", err, errTooManyConnections)
	}

	cancel()
	deadline := time.After(time.Second)
	for {
		ctx2, cancel2 := context.WithCancel(context.Background())
		_, err = srv.connContext(ctx2, &quicgo.ClientInfo{RemoteAddr: addr})
		if err == nil {
			cancel2()
			return
		}
		cancel2()
		select {
		case <-deadline:
			t.Fatalf("slot was not released after context cancel: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestQueryAnswerLoopback(t *testing.T) {
	serverKey := mustKey(t)
	clientKey := mustKey(t)
	clientPub := clientKey.Public().(ed25519.PublicKey)
	clientID := adnlIDFromKey(clientPub)

	var gotFrom ed25519.PublicKey
	var gotPayload []byte
	var mu sync.Mutex
	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			mu.Lock()
			gotFrom = append(ed25519.PublicKey(nil), from...)
			gotPayload = append([]byte(nil), payload...)
			mu.Unlock()
			// answer = "echo:" + request
			return append([]byte("echo:"), payload...), nil
		},
	}

	addr, srv := startServer(t, handler, serverKey)
	serverPub := srv.defaultID.PublicKey()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli, err := Dial(ctx, addr, clientKey, serverPub)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()

	if !bytes.Equal(cli.PeerID(), srv.defaultID.ID()) {
		t.Fatalf("client PeerID mismatch")
	}

	req := []byte("hello ton quic")
	ans, err := cli.Query(ctx, req, 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if want := append([]byte("echo:"), req...); !bytes.Equal(ans, want) {
		t.Fatalf("answer = %q, want %q", ans, want)
	}

	mu.Lock()
	defer mu.Unlock()
	if !bytes.Equal(gotFrom, clientPub) {
		t.Fatalf("server saw from=%s, want client %s", gotFrom, clientID)
	}
	if !bytes.Equal(gotPayload, req) {
		t.Fatalf("server saw payload %q, want %q", gotPayload, req)
	}
}

func TestQueryRejectsRequestOverServerLimit(t *testing.T) {
	serverKey := mustKey(t)
	clientKey := mustKey(t)

	var called atomic.Bool
	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			called.Store(true)
			return payload, nil
		},
	}

	payload := bytes.Repeat([]byte{0xAA}, 2048)
	wire, err := serializeBoxed(idQuicQuery, payload)
	if err != nil {
		t.Fatal(err)
	}

	addr, srv := startServerWithConfig(t, handler, func(s *Server) {
		s.maxObjectSize = int64(len(wire) - 1)
	}, serverKey)
	serverPub := srv.defaultID.PublicKey()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli, err := Dial(ctx, addr, clientKey, serverPub)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()

	if _, err = cli.Query(ctx, payload, 0); err == nil {
		t.Fatal("expected oversized request to fail")
	}
	if called.Load() {
		t.Fatal("handler should not be called for oversized request")
	}
}

func TestQueryMaxAnswerOverridesDefaultLimit(t *testing.T) {
	serverKey := mustKey(t)
	clientKey := mustKey(t)
	answer := bytes.Repeat([]byte{0xBB}, 2048)

	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return answer, nil
		},
	}

	addr, srv := startServer(t, handler, serverKey)
	serverPub := srv.defaultID.PublicKey()

	wire, err := serializeBoxed(idQuicAnswer, answer)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli, err := Dial(ctx, addr, clientKey, serverPub)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	cli.maxObjectSize = int64(len(wire) - 1)

	if _, err = cli.Query(ctx, []byte("small"), 0); err == nil {
		t.Fatal("expected default answer limit to reject oversized answer")
	}

	cli2, err := Dial(ctx, addr, clientKey, serverPub)
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}
	defer cli2.Close()
	cli2.maxObjectSize = int64(len(wire) - 1)

	got, err := cli2.Query(ctx, []byte("small"), int64(len(wire)))
	if err != nil {
		t.Fatalf("query with maxAnswer: %v", err)
	}
	if !bytes.Equal(got, answer) {
		t.Fatal("answer mismatch")
	}
}

func TestMessageLoopback(t *testing.T) {
	serverKey := mustKey(t)
	clientKey := mustKey(t)
	clientPub := clientKey.Public().(ed25519.PublicKey)

	recv := make(chan struct {
		from    ed25519.PublicKey
		payload []byte
	}, 1)
	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return nil, nil
		},
		OnMessage: func(ctx context.Context, from ed25519.PublicKey, payload []byte) {
			recv <- struct {
				from    ed25519.PublicKey
				payload []byte
			}{append(ed25519.PublicKey(nil), from...), append([]byte(nil), payload...)}
		},
	}

	addr, srv := startServer(t, handler, serverKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli, err := Dial(ctx, addr, clientKey, srv.defaultID.PublicKey())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()

	msg := []byte("fire and forget")
	if err := cli.SendMessage(ctx, msg); err != nil {
		t.Fatalf("send message: %v", err)
	}

	select {
	case got := <-recv:
		if !bytes.Equal(got.from, clientPub) {
			t.Fatalf("message from %x, want %x", got.from, clientPub)
		}
		if !bytes.Equal(got.payload, msg) {
			t.Fatalf("message payload %q, want %q", got.payload, msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message delivery")
	}
}

// TestWrongPeerRejected ensures dialing with a mismatched expected id fails.
func TestWrongPeerRejected(t *testing.T) {
	serverKey := mustKey(t)
	clientKey := mustKey(t)
	handler := Handler{OnQuery: func(context.Context, ed25519.PublicKey, []byte) ([]byte, error) { return nil, nil }}
	addr, _ := startServer(t, handler, serverKey)

	// Expect a random (wrong) peer key.
	wrong := mustKey(t).Public().(ed25519.PublicKey)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Dial(ctx, addr, clientKey, wrong); err == nil {
		t.Fatal("expected dial to fail against wrong expected peer id")
	}
}

func TestUnknownSNIRejected(t *testing.T) {
	serverKey := mustKey(t)
	clientKey := mustKey(t)
	handler := Handler{OnQuery: func(context.Context, ed25519.PublicKey, []byte) ([]byte, error) { return nil, nil }}
	addr, _ := startServer(t, handler, serverKey)

	unknown := adnlIDFromKey(mustKey(t).Public().(ed25519.PublicKey)).sni()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialWithServerName(ctx, addr, clientKey, unknown)
	if err == nil {
		_ = conn.CloseWithError(0, "")
		t.Fatal("expected unknown SNI to be rejected")
	}
}

func dialWithServerName(ctx context.Context, addr string, localKey ed25519.PrivateKey, serverName string) (*quicgo.Conn, error) {
	local, err := NewIdentity(localKey)
	if err != nil {
		return nil, err
	}
	tlsConf := &forktls.Config{
		MinVersion:             forktls.VersionTLS13,
		NextProtos:             []string{ALPN},
		ServerName:             serverName,
		SessionTicketsDisabled: true,
		RawPublicKeys: &forktls.RawPublicKeyConfig{
			PrivateKey: local.key,
			Verify:     func(ed25519.PublicKey) error { return nil },
		},
	}
	return quicgo.DialAddr(ctx, addr, tlsConf, defaultQUICConfig())
}

// TestMultiIdentitySNI verifies a server hosting two identities routes by SNI.
func TestMultiIdentitySNI(t *testing.T) {
	key1 := mustKey(t)
	key2 := mustKey(t)
	clientKey := mustKey(t)

	handler := Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return payload, nil
		},
	}
	addr, srv := startServer(t, handler, key1, key2)
	id2, err := NewIdentity(key2)
	if err != nil {
		t.Fatal(err)
	}
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Dial the *second* identity explicitly; SNI must route to key2.
	cli, err := Dial(ctx, addr, clientKey, id2.PublicKey())
	if err != nil {
		t.Fatalf("dial identity 2: %v", err)
	}
	defer cli.Close()
	if !bytes.Equal(cli.PeerID(), id2.ID()) {
		t.Fatalf("expected to reach identity 2")
	}
	ans, err := cli.Query(ctx, []byte("ping"), 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !bytes.Equal(ans, []byte("ping")) {
		t.Fatalf("bad echo answer")
	}
}
