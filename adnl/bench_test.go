package adnl

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

const (
	// A plain flexserver Plumtree IHAVE is 204 bytes boxed. Its overlay.message
	// envelope adds 36 bytes, which is the payload passed to either transport.
	plumtreeIHavePayloadSize = 240
	adnlBenchMessageDataSize = 232
	adnlBenchmarkMaxLag      = 4 * 1024
	adnlBenchmarkResumeLag   = adnlBenchmarkMaxLag / 2
)

type ADNLBenchMessage struct {
	Data []byte `tl:"bytes"`
}

func init() {
	tl.Register(ADNLBenchMessage{}, "bench.adnl.message data:bytes = bench.adnl.Message")
}

type adnlBenchPacketCounter struct {
	received atomic.Uint64
	target   atomic.Uint64
	reached  chan struct{}
}

func newADNLBenchPacketCounter() *adnlBenchPacketCounter {
	return &adnlBenchPacketCounter{reached: make(chan struct{}, 1)}
}

func (c *adnlBenchPacketCounter) handle(*MessageCustom) error {
	received := c.received.Add(1)
	if received == c.target.Load() {
		select {
		case c.reached <- struct{}{}:
		default:
		}
	}
	return nil
}

func (c *adnlBenchPacketCounter) expect(target uint64) {
	for {
		select {
		case <-c.reached:
		default:
			c.target.Store(target)
			return
		}
	}
}

func (c *adnlBenchPacketCounter) wait(ctx context.Context, target uint64) error {
	for c.received.Load() < target {
		select {
		case <-c.reached:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func benchmarkFreeUDPPort(b *testing.B) int {
	b.Helper()

	lp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer lp.Close()

	return lp.LocalAddr().(*net.UDPAddr).Port
}

func setupADNLQueryBenchmark(b *testing.B) Peer {
	b.Helper()

	port := benchmarkFreeUDPPort(b)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	srvPub, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	_, cliKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}

	srv := NewGateway(srvKey)
	if err = srv.StartServer(addr); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = srv.Close()
	})

	srv.SetConnectionHandler(func(client Peer) error {
		client.SetQueryHandler(func(msg *MessageQuery) error {
			switch m := msg.Data.(type) {
			case MessagePing:
				return client.Answer(context.Background(), msg.ID, MessagePong{Value: m.Value})
			default:
				return nil
			}
		})
		return nil
	})

	cliGw := NewGateway(cliKey)
	if err = cliGw.StartClient(); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = cliGw.Close()
	})

	cli, err := cliGw.RegisterClient(addr, srvPub)
	if err != nil {
		b.Fatal(err)
	}

	var pong MessagePong
	if err = cli.Query(context.Background(), &MessagePing{Value: 1}, &pong); err != nil {
		b.Fatal(err)
	}

	return cli
}

func setupADNLCustomMessageBenchmark(b *testing.B) (Peer, tl.Raw, *adnlBenchPacketCounter) {
	b.Helper()

	port := benchmarkFreeUDPPort(b)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	srvPub, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	_, cliKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}

	counter := newADNLBenchPacketCounter()
	srv := NewGateway(srvKey)
	srv.SetConnectionHandler(func(client Peer) error {
		client.SetCustomMessageHandler(counter.handle)
		return nil
	})
	if err = srv.StartServer(addr); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = srv.Close()
	})

	cliGw := NewGateway(cliKey)
	if err = cliGw.StartClient(); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = cliGw.Close()
	})

	cli, err := cliGw.RegisterClient(addr, srvPub)
	if err != nil {
		b.Fatal(err)
	}

	payload, err := tl.Serialize(ADNLBenchMessage{
		Data: make([]byte, adnlBenchMessageDataSize),
	}, true)
	if err != nil {
		b.Fatal(err)
	}
	if len(payload) != plumtreeIHavePayloadSize {
		b.Fatalf("benchmark payload is %d bytes, want %d", len(payload), plumtreeIHavePayloadSize)
	}

	warmupCtx, warmupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer warmupCancel()

	warmupTarget := counter.received.Load() + 1
	counter.expect(warmupTarget)
	if err = cli.SendCustomMessage(warmupCtx, tl.Raw(payload)); err != nil {
		b.Fatalf("send warmup custom message: %v", err)
	}
	if err = counter.wait(warmupCtx, warmupTarget); err != nil {
		b.Fatalf("receive warmup custom message: %v", err)
	}
	if err = waitADNLBenchmarkChannel(warmupCtx, cli); err != nil {
		b.Fatalf("open ADNL channel: %v", err)
	}

	return cli, tl.Raw(payload), counter
}

func waitADNLBenchmarkChannel(ctx context.Context, peer Peer) error {
	conn := peer.(*peerConn)
	client := conn.client.(*ADNL)

	for {
		channel := (*Channel)(atomic.LoadPointer(&client.channelPtr))
		if channel != nil && channel.ready.Load() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			runtime.Gosched()
		}
	}
}

func BenchmarkADNLCustomMessageLoopback(b *testing.B) {
	cli, payload, counter := setupADNLCustomMessageBenchmark(b)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	start := counter.received.Load()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := cli.SendCustomMessage(ctx, payload); err != nil {
			b.Fatal(err)
		}

		submitted := uint64(i + 1)
		received := counter.received.Load() - start
		// Keep the asynchronous receiver saturated without allowing an unbounded
		// transport queue. This is aggregate flow control, not a per-message reply.
		if submitted-min(submitted, received) >= adnlBenchmarkMaxLag {
			target := start + submitted - adnlBenchmarkResumeLag
			counter.expect(target)
			if err := counter.wait(ctx, target); err != nil {
				b.Fatalf("wait for ADNL receiver lag: %v", err)
			}
		}
	}
	b.StopTimer()

	elapsed := b.Elapsed().Seconds()
	received := counter.received.Load() - start
	if received > 0 {
		b.ReportMetric(elapsed*1e9/float64(received), "ns/message")
		b.ReportMetric(float64(received)/elapsed, "messages/s")
		b.ReportMetric(float64(received*uint64(len(payload)))/elapsed/1e6, "payload_MB/s")
	}
	b.ReportMetric(float64(received), "received")
	b.ReportMetric(float64(b.N), "submitted")
	b.ReportMetric(adnlBenchmarkMaxLag, "max_lag")
	if b.N > 0 {
		b.ReportMetric(100*float64(received)/float64(b.N), "delivery_pct")
	}

	drainTarget := start + uint64(b.N)
	counter.expect(drainTarget)
	if err := counter.wait(ctx, drainTarget); err != nil {
		b.Fatalf("drain ADNL receiver after measurement: %v", err)
	}
}

func BenchmarkADNLQueryLoopback(b *testing.B) {
	cli := setupADNLQueryBenchmark(b)
	ctx := context.Background()
	var res MessagePong

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cli.Query(ctx, &MessagePing{Value: int64(i)}, &res); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkADNLQueryLoopbackParallel(b *testing.B) {
	cli := setupADNLQueryBenchmark(b)
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		var res MessagePong
		var ctr int64
		for pb.Next() {
			ctr++
			if err := cli.Query(context.Background(), &MessagePing{Value: ctr}, &res); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkGatewayGetID(b *testing.B) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	gw := NewGateway(key)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gw.GetID()
	}
}

func BenchmarkPeerStats(b *testing.B) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	peer := NewGateway(key).initADNL()

	b.ReportAllocs()
	for b.Loop() {
		_ = peer.Stats()
	}
}

func BenchmarkADNLQueryRegistryKey(b *testing.B) {
	id := make([]byte, 32)
	for i := range id {
		id[i] = byte(i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := encodeQueryID(id); !ok {
			b.Fatal("unexpected query id size")
		}
	}
}

func BenchmarkCreatePacket(b *testing.B) {
	peerPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	_, ourPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}

	a := NewGateway(ourPriv).initADNL()
	a.peerKey = peerPub
	a.peerID, err = tl.Hash(keys.PublicKeyED25519{Key: peerPub})
	if err != nil {
		b.Fatal(err)
	}
	a.peerKeyX25519, err = keys.Ed25519PubToX25519(peerPub)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := a.createPacket(int64(i+1), MessageNop{}); err != nil {
			b.Fatal(err)
		}
	}
}
