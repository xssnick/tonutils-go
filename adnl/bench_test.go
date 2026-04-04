package adnl

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"testing"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

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
