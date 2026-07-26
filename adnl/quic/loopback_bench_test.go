package quic

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

type QuicBenchRequest struct {
	WantLen uint32 `tl:"int"`
}

func init() {
	tl.Register(QuicBenchRequest{}, "bench.quic.request want_len:int = bench.quic.Request")
}

type quicBenchQuery func(ctx context.Context, payload []byte, maxAnswer int64) ([]byte, error)

var quicBenchSink []byte

type quicBenchCase struct {
	name      string
	request   []byte
	response  []byte
	maxAnswer int64
}

type quicBenchFixture struct {
	responses map[uint32][]byte
}

func BenchmarkQUICSteadyStateQuery(b *testing.B) {
	cases := newQUICBenchCases(b)

	scenarios := []struct {
		name  string
		setup func(*testing.B, func([]byte) ([]byte, error)) (quicBenchQuery, func())
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
		b.Run(sc.name, func(b *testing.B) {
			fixture := newQUICBenchFixture(cases)
			query, cleanup := sc.setup(b, fixture.handleQuery)
			defer cleanup()

			for _, tc := range cases {
				for _, concurrency := range []int{1, 16, 64} {
					b.Run(fmt.Sprintf("size=%s/concurrency=%d", tc.name, concurrency), func(b *testing.B) {
						runQUICSteadyStateQuery(b, query, tc, concurrency)
					})
				}
			}
		})
	}
}

func newQUICBenchCases(tb testing.TB) []quicBenchCase {
	tb.Helper()

	sizes := []struct {
		name string
		size int
	}{
		{name: "1KiB", size: 1 << 10},
		{name: "64KiB", size: 64 << 10},
		{name: "1MiB", size: 1 << 20},
		{name: "plumtree_max", size: MaxPlumtreePayloadSize},
		{name: "10MiB", size: 10 << 20},
	}

	cases := make([]quicBenchCase, len(sizes))
	for i, size := range sizes {
		request, err := tl.Serialize(QuicBenchRequest{WantLen: uint32(size.size)}, true)
		if err != nil {
			tb.Fatal(err)
		}
		_, _, _, maxAnswer, err := boxedObjectHeader(idQuicAnswer, size.size)
		if err != nil {
			tb.Fatal(err)
		}
		cases[i] = quicBenchCase{
			name:      size.name,
			request:   request,
			response:  make([]byte, size.size),
			maxAnswer: int64(maxAnswer),
		}
	}
	return cases
}

func newQUICBenchFixture(cases []quicBenchCase) *quicBenchFixture {
	responses := make(map[uint32][]byte, len(cases))
	for _, tc := range cases {
		responses[uint32(len(tc.response))] = tc.response
	}
	return &quicBenchFixture{responses: responses}
}

func (f *quicBenchFixture) handleQuery(payload []byte) ([]byte, error) {
	var req QuicBenchRequest
	rest, err := tl.ParseNoCopy(&req, payload, true)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%d trailing bytes after request", len(rest))
	}

	response, ok := f.responses[req.WantLen]
	if !ok {
		return nil, fmt.Errorf("unsupported benchmark response length %d", req.WantLen)
	}
	return response, nil
}

func runQUICSteadyStateQuery(b *testing.B, query quicBenchQuery, tc quicBenchCase, concurrency int) {
	b.Helper()

	warmupCtx, warmupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	answer, err := query(warmupCtx, tc.request, tc.maxAnswer)
	warmupCancel()
	if err != nil {
		b.Fatalf("warmup query: %v", err)
	}
	if len(answer) != len(tc.response) {
		b.Fatalf("warmup response length = %d, want %d", len(answer), len(tc.response))
	}
	quicBenchSink = answer

	b.ReportAllocs()
	b.SetBytes(int64(len(tc.response)))
	b.StopTimer()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var workers sync.WaitGroup
	workers.Add(concurrency)
	queryErrors := make(chan error, concurrency)
	sinks := make([][]byte, concurrency)
	start := make(chan struct{})

	baseIterations := b.N / concurrency
	extraIterations := b.N % concurrency
	for worker := range concurrency {
		iterations := baseIterations
		if worker < extraIterations {
			iterations++
		}

		go func() {
			defer workers.Done()
			<-start

			var last []byte
			for range iterations {
				answer, queryErr := query(ctx, tc.request, tc.maxAnswer)
				if queryErr != nil {
					queryErrors <- queryErr
					cancel()
					return
				}
				if len(answer) != len(tc.response) {
					queryErrors <- fmt.Errorf("response length = %d, want %d", len(answer), len(tc.response))
					cancel()
					return
				}
				last = answer
			}
			sinks[worker] = last
		}()
	}

	b.ResetTimer()
	b.StartTimer()
	close(start)
	workers.Wait()
	b.StopTimer()

	close(queryErrors)
	for err = range queryErrors {
		b.Fatal(err)
	}
	for _, sink := range sinks {
		if sink != nil {
			quicBenchSink = sink
			break
		}
	}
}

func setupTransportLoopbackBenchmark(b *testing.B, handleQuery func([]byte) ([]byte, error)) (quicBenchQuery, func()) {
	b.Helper()

	serverKey := mustBenchKey(b)
	clientKey := mustBenchKey(b)

	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		b.Fatal(err)
	}

	srv, err := NewServer(Handler{
		OnQuery: func(ctx context.Context, from ed25519.PublicKey, payload []byte) ([]byte, error) {
			return handleQuery(payload)
		},
	}, serverKey)
	if err != nil {
		_ = pc.Close()
		b.Fatal(err)
	}

	go func() { _ = srv.Serve(pc) }()

	readyCtx, readyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = srv.WaitReady(readyCtx)
	readyCancel()
	if err != nil {
		_ = srv.Close()
		_ = pc.Close()
		b.Fatal(err)
	}

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

func setupGatewayLoopbackBenchmark(b *testing.B, handleQuery func([]byte) ([]byte, error)) (quicBenchQuery, func()) {
	b.Helper()

	serverKey := mustBenchKey(b)
	clientKey := mustBenchKey(b)

	server, err := NewGateway(serverKey)
	if err != nil {
		b.Fatal(err)
	}
	server.SetConnectionHandler(func(peer *Peer) error {
		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			return handleQuery(payload)
		})
		return nil
	})

	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_ = server.Close()
		b.Fatal(err)
	}
	go func() { _ = server.Serve(pc) }()

	readyCtx, readyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = server.WaitReady(readyCtx)
	readyCancel()
	if err != nil {
		_ = server.Close()
		_ = pc.Close()
		b.Fatal(err)
	}

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
	cases := []quicBenchCase{{
		name:     "128B",
		request:  mustSerializeBenchRequest(t, 128),
		response: make([]byte, 128),
	}}
	fixture := newQUICBenchFixture(cases)

	response, err := fixture.handleQuery(cases[0].request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response, make([]byte, 128)) {
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
