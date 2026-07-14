package quic

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

type QuicBenchRequest struct {
	WantLen uint32 `tl:"int"`
}

type QuicBenchResponse struct {
	Data []byte `tl:"bytes"`
}

func init() {
	tl.Register(QuicBenchRequest{}, "bench.quic.request want_len:int = bench.quic.Request")
	tl.Register(QuicBenchResponse{}, "bench.quic.response data:bytes = bench.quic.Response")
}

type quicBenchQuery func(ctx context.Context, payload []byte, maxAnswer int64) ([]byte, error)

var quicBenchSink []byte

func BenchmarkQUIC_ClientServer(b *testing.B) {
	sizes := []uint32{16 << 10, 256 << 10, 1 << 20, 4 << 20, 10 << 20}

	scenarios := []struct {
		name  string
		setup func(*testing.B) (quicBenchQuery, func())
	}{
		{
			name:  "transport_loopback",
			setup: setupTransportLoopbackBenchmark,
		},
		{
			name:  "gateway_loopback",
			setup: setupGatewayLoopbackBenchmark,
		},
	}

	for _, sc := range scenarios {
		sc := sc
		b.Run(sc.name, func(b *testing.B) {
			query, cleanup := sc.setup(b)
			defer cleanup()

			runQUICBenchSizes(b, query, sizes, true)
		})
	}
}

func runQUICBenchSizes(b *testing.B, query quicBenchQuery, sizes []uint32, withParallel bool) {
	for _, sz := range sizes {
		b.Run(fmt.Sprintf("resp=%dKB", sz>>10), func(b *testing.B) {
			ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
			if err := doQUICBenchQuery(ctx, query, sz); err != nil {
				cancel()
				b.Fatalf("warmup query: %v", err)
			}
			cancel()

			b.SetBytes(int64(sz))
			for b.Loop() {
				ctx, cancel = context.WithTimeout(context.Background(), 7*time.Second)
				err := doQUICBenchQuery(ctx, query, sz)
				cancel()
				if err != nil {
					b.Fatalf("query: %v", err)
				}
			}
		})

		if withParallel {
			b.Run(fmt.Sprintf("resp=%dKB/parallel", sz>>10), func(b *testing.B) {
				ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
				if err := doQUICBenchQuery(ctx, query, sz); err != nil {
					cancel()
					b.Fatalf("warmup query: %v", err)
				}
				cancel()

				b.SetBytes(int64(sz))
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
						err := doQUICBenchQuery(ctx, query, sz)
						cancel()
						if err != nil {
							b.Fatalf("query: %v", err)
						}
					}
				})
			})
		}
	}
}

func doQUICBenchQuery(ctx context.Context, query quicBenchQuery, sz uint32) error {
	req, err := tl.Serialize(QuicBenchRequest{WantLen: sz}, true)
	if err != nil {
		return err
	}

	answer, err := query(ctx, req, int64(sz)+4096)
	if err != nil {
		return err
	}

	var resp QuicBenchResponse
	rest, err := tl.ParseNoCopy(&resp, answer, true)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("%d trailing bytes after response", len(rest))
	}
	if len(resp.Data) != int(sz) {
		return fmt.Errorf("response length = %d, want %d", len(resp.Data), sz)
	}

	quicBenchSink = resp.Data
	return nil
}

func handleQUICBenchQuery(payload []byte) ([]byte, error) {
	var req QuicBenchRequest
	rest, err := tl.ParseNoCopy(&req, payload, true)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%d trailing bytes after request", len(rest))
	}

	return tl.Serialize(QuicBenchResponse{Data: make([]byte, req.WantLen)}, true)
}

func setupTransportLoopbackBenchmark(b *testing.B) (quicBenchQuery, func()) {
	b.Helper()

	serverKey := mustBenchKey(b)
	clientKey := mustBenchKey(b)

	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		b.Fatal(err)
	}

	srv, err := NewServer(Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return handleQUICBenchQuery(payload)
		},
	}, serverKey)
	if err != nil {
		_ = pc.Close()
		b.Fatal(err)
	}

	go func() { _ = srv.Serve(pc) }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	client, err := Dial(ctx, pc.LocalAddr().String(), clientKey, srv.defaultID.PublicKey())
	cancel()
	if err != nil {
		_ = srv.Close()
		_ = pc.Close()
		b.Fatal(err)
	}

	cleanup := func() {
		_ = client.Close()
		_ = srv.Close()
		_ = pc.Close()
	}
	return client.Query, cleanup
}

func setupGatewayLoopbackBenchmark(b *testing.B) (quicBenchQuery, func()) {
	b.Helper()

	serverKey := mustBenchKey(b)
	clientKey := mustBenchKey(b)

	server, err := NewGateway(serverKey)
	if err != nil {
		b.Fatal(err)
	}
	server.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			return handleQUICBenchQuery(payload)
		})
		return nil
	})

	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_ = server.Close()
		b.Fatal(err)
	}
	go func() { _ = server.Serve(pc) }()

	client, err := NewGateway(clientKey)
	if err != nil {
		_ = server.Close()
		_ = pc.Close()
		b.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	peer, err := client.DialDefault(ctx, server.PublicKey(), pc.LocalAddr().String())
	cancel()
	if err != nil {
		_ = client.Close()
		_ = server.Close()
		_ = pc.Close()
		b.Fatal(err)
	}

	cleanup := func() {
		_ = peer.Close()
		_ = client.Close()
		_ = server.Close()
		_ = pc.Close()
	}
	return peer.Query, cleanup
}

func mustBenchKey(b *testing.B) ed25519.PrivateKey {
	b.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	return priv
}

func TestQUICBenchRequestRoundTrip(t *testing.T) {
	wire, err := handleQUICBenchQuery(mustSerializeBenchRequest(t, 128))
	if err != nil {
		t.Fatal(err)
	}

	var resp QuicBenchResponse
	rest, err := tl.ParseNoCopy(&resp, wire, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 0 {
		t.Fatalf("%d trailing bytes after response", len(rest))
	}
	if !bytes.Equal(resp.Data, make([]byte, 128)) {
		t.Fatalf("response payload mismatch")
	}
}

func mustSerializeBenchRequest(t *testing.T, sz uint32) []byte {
	t.Helper()

	wire, err := tl.Serialize(QuicBenchRequest{WantLen: sz}, true)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}
