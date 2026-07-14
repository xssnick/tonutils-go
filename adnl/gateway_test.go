package adnl

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
)

func TestGatewayLimitsPendingPeersLRU(t *testing.T) {
	_, gatewayKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	gateway := NewGateway(gatewayKey)
	gateway.SetAddressList(nil)
	gateway.maxPendingPeers = 3
	gateway.pendingPeerTTL = time.Hour
	defer closeGatewayPeersForTest(gateway)

	pubKeys := make([]ed25519.PublicKey, 0, 4)
	ids := make([]string, 0, 4)
	for i := 0; i < 3; i++ {
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}

		peer, err := gateway.RegisterClient("127.0.0.1:12345", pub)
		if err != nil {
			t.Fatal(err)
		}

		pubKeys = append(pubKeys, pub)
		ids = append(ids, string(peer.GetID()))
	}

	if _, err = gateway.RegisterClient("127.0.0.1:12345", pubKeys[0]); err != nil {
		t.Fatal(err)
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := gateway.RegisterClient("127.0.0.1:12345", pub)
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, string(peer.GetID()))

	gateway.mx.RLock()
	_, hasFirst := gateway.peers[ids[0]]
	_, hasSecond := gateway.peers[ids[1]]
	_, hasThird := gateway.peers[ids[2]]
	_, hasFourth := gateway.peers[ids[3]]
	peers := len(gateway.peers)
	pending := gateway.pendingPeers.Len()
	gateway.mx.RUnlock()

	if peers != gateway.maxPendingPeers {
		t.Fatalf("unexpected peer count: %d", peers)
	}
	if pending != gateway.maxPendingPeers {
		t.Fatalf("unexpected pending peer count: %d", pending)
	}
	if !hasFirst || hasSecond || !hasThird || !hasFourth {
		t.Fatalf("unexpected LRU state: first=%v second=%v third=%v fourth=%v", hasFirst, hasSecond, hasThird, hasFourth)
	}
}

func TestGatewayCollectsIdlePendingPeers(t *testing.T) {
	_, gatewayKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	gateway := NewGateway(gatewayKey)
	gateway.SetAddressList(nil)
	gateway.maxPendingPeers = 10
	gateway.pendingPeerTTL = time.Minute
	defer closeGatewayPeersForTest(gateway)

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	idlePeer, err := gateway.RegisterClient("127.0.0.1:12345", pub)
	if err != nil {
		t.Fatal(err)
	}

	pub, _, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	activePeer, err := gateway.RegisterClient("127.0.0.1:12345", pub)
	if err != nil {
		t.Fatal(err)
	}

	atomic.StoreInt64(&idlePeer.(*peerConn).lastPacketAt, time.Now().Add(-2*time.Minute).UnixNano())

	gateway.mx.Lock()
	idle := gateway.collectIdlePendingPeersLocked(time.Now().UnixNano())
	gateway.mx.Unlock()
	closePeers(idle)

	gateway.mx.RLock()
	_, hasIdle := gateway.peers[string(idlePeer.GetID())]
	_, hasActive := gateway.peers[string(activePeer.GetID())]
	peers := len(gateway.peers)
	pending := gateway.pendingPeers.Len()
	gateway.mx.RUnlock()

	if hasIdle || !hasActive {
		t.Fatalf("unexpected idle cleanup state: idle=%v active=%v", hasIdle, hasActive)
	}
	if peers != 1 || pending != 1 {
		t.Fatalf("unexpected peer counts after idle cleanup: peers=%d pending=%d", peers, pending)
	}
}

func TestGatewayLimitsFreshRootPeers(t *testing.T) {
	_, serverPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	addr := reserveUDPAddrForTest(t)
	gateway := NewGateway(serverPriv)
	gateway.maxPendingPeers = 4
	gateway.pendingPeerTTL = time.Hour
	defer gateway.Close()

	var accepted int32
	gateway.SetConnectionHandler(func(client Peer) error {
		atomic.AddInt32(&accepted, 1)
		return nil
	})

	if err = gateway.StartServer(addr); err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	const floodPeers = 20
	for i := 0; i < floodPeers; i++ {
		senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		_, outerPriv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}

		packet := buildEncryptedRootPacket(t, serverPriv, senderPriv, outerPriv, &PacketContent{
			Rand1:        []byte{1, 2, 3, 4, 5, 6, 7},
			From:         &keys.PublicKeyED25519{Key: senderPub},
			Messages:     []any{MessageNop{}},
			Seqno:        int64Ptr(int64(i + 1)),
			ConfirmSeqno: int64Ptr(0),
			Rand2:        []byte{8, 9, 10, 11, 12, 13, 14},
		})

		if _, err = conn.Write(packet); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&accepted) < floodPeers {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&accepted); got < floodPeers {
		t.Fatalf("gateway processed %d root packets, want %d", got, floodPeers)
	}

	gateway.mx.RLock()
	peers := len(gateway.peers)
	pending := gateway.pendingPeers.Len()
	gateway.mx.RUnlock()

	if peers > gateway.maxPendingPeers {
		t.Fatalf("peer table exceeded limit: %d > %d", peers, gateway.maxPendingPeers)
	}
	if pending > gateway.maxPendingPeers {
		t.Fatalf("pending peer table exceeded limit: %d > %d", pending, gateway.maxPendingPeers)
	}
}

func closeGatewayPeersForTest(gateway *Gateway) {
	gateway.mx.RLock()
	peers := make([]*peerConn, 0, len(gateway.peers))
	for _, peer := range gateway.peers {
		peers = append(peers, peer)
	}
	gateway.mx.RUnlock()

	closePeers(peers)
}

func reserveUDPAddrForTest(t *testing.T) string {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
