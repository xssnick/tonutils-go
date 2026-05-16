package overlay

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

type mockBroadcastPeer struct {
	id       []byte
	sent     []tl.Serializable
	sendErr  error
	sendFunc func(ctx context.Context, req tl.Serializable) error
}

func (m *mockBroadcastPeer) ID() []byte {
	return m.id
}

func (m *mockBroadcastPeer) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	m.sent = append(m.sent, req)
	if m.sendFunc != nil {
		return m.sendFunc(ctx, req)
	}
	return m.sendErr
}

type mockBroadcastPeerSet struct {
	peers []BroadcastPeer
}

func (m mockBroadcastPeerSet) Peers() []BroadcastPeer {
	return m.peers
}

func TestBroadcastFECSignAndShortHelpers(t *testing.T) {
	_, priv := keyPairFromSeed(51)
	payload := []byte("payload")
	dataHash := calcBroadcastFECPartDataHash(payload)

	full := &BroadcastFEC{
		Source:      ed25519Public(priv),
		Certificate: CertificateEmpty{},
		DataHash:    dataHash,
		DataSize:    uint32(len(payload)),
		Flags:       BroadcastFlagAnySender,
		Data:        []byte("symbol"),
		Seqno:       7,
		FEC:         raptorQForTests(len(payload), 3),
		Date:        uint32(time.Now().Unix()),
	}
	if err := full.Sign(priv); err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	if err := full.VerifySignature(); err != nil {
		t.Fatalf("verify failed: %v", err)
	}

	short, err := full.Short()
	if err != nil {
		t.Fatalf("short build failed: %v", err)
	}
	if err = short.VerifySignature(full.Date); err != nil {
		t.Fatalf("short verify failed: %v", err)
	}

	fullPartID, err := full.CalcPartID()
	if err != nil {
		t.Fatalf("full part id failed: %v", err)
	}
	shortPartID, err := short.CalcPartID()
	if err != nil {
		t.Fatalf("short part id failed: %v", err)
	}
	if !bytes.Equal(fullPartID, shortPartID) {
		t.Fatalf("expected full and short part ids to match")
	}
}

func TestBroadcastFECSenderTracksPeerState(t *testing.T) {
	now := time.Unix(1000, 0)
	_, priv := keyPairFromSeed(52)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x41}, 32)}, BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(24),
		WithBroadcastFECBurstSize(1),
		withBroadcastFECNow(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	peer1 := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x01}, 32)}
	peer2 := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x02}, 32)}
	peers := mockBroadcastPeerSet{peers: []BroadcastPeer{peer1, peer2}}

	if sent, err := sender.SendNow(context.Background(), peers, 1); err != nil || sent != 1 {
		t.Fatalf("first send failed, sent=%d err=%v", sent, err)
	}
	if _, ok := peer1.sent[0].(*BroadcastFEC); !ok {
		t.Fatalf("expected full part for peer1 on first send, got %T", peer1.sent[0])
	}
	if _, ok := peer2.sent[0].(*BroadcastFEC); !ok {
		t.Fatalf("expected full part for peer2 on first send, got %T", peer2.sent[0])
	}

	hash := sender.BroadcastHash()
	if !sender.TrackControlMessage(peer1.id, FECReceived{Hash: hash}) {
		t.Fatalf("expected FECReceived to be tracked")
	}
	if sender.TrackControlMessage(peer1.id, FECReceived{Hash: bytes.Repeat([]byte{0x99}, 32)}) {
		t.Fatalf("wrong hash should not be tracked")
	}

	if sent, err := sender.SendNow(context.Background(), peers, 1); err != nil || sent != 1 {
		t.Fatalf("second send failed, sent=%d err=%v", sent, err)
	}
	if _, ok := peer1.sent[1].(*BroadcastFECShort); !ok {
		t.Fatalf("expected short part for received peer, got %T", peer1.sent[1])
	}
	if _, ok := peer2.sent[1].(*BroadcastFEC); !ok {
		t.Fatalf("expected full part for unconfirmed peer, got %T", peer2.sent[1])
	}

	if !sender.TrackControlMessage(peer1.id, FECCompleted{Hash: hash}) {
		t.Fatalf("expected FECCompleted to be tracked")
	}
	if sent, err := sender.SendNow(context.Background(), peers, 1); err != nil || sent != 1 {
		t.Fatalf("third send failed, sent=%d err=%v", sent, err)
	}
	if len(peer1.sent) != 2 {
		t.Fatalf("completed peer must not receive more parts, got %d messages", len(peer1.sent))
	}
	if len(peer2.sent) != 3 {
		t.Fatalf("active peer must keep receiving parts, got %d messages", len(peer2.sent))
	}
}

func TestBroadcastFECSenderRespectsPacingAndExpiry(t *testing.T) {
	now := time.Unix(2000, 0)
	_, priv := keyPairFromSeed(53)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x42}, 32)}, BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(24),
		WithBroadcastFECBurstSize(2),
		WithBroadcastFECPace(time.Second),
		WithBroadcastFECTTL(2*time.Second),
		withBroadcastFECNow(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	peer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x03}, 32)}
	peers := mockBroadcastPeerSet{peers: []BroadcastPeer{peer}}

	if sent, err := sender.SendReady(context.Background(), peers); err != nil || sent != 2 {
		t.Fatalf("first paced send failed, sent=%d err=%v", sent, err)
	}
	if sent, err := sender.SendReady(context.Background(), peers); err != nil || sent != 0 {
		t.Fatalf("sender must wait for pace, sent=%d err=%v", sent, err)
	}

	now = now.Add(time.Second)
	if sent, err := sender.SendReady(context.Background(), peers); err != nil || sent == 0 {
		t.Fatalf("sender should send after pace elapsed, sent=%d err=%v", sent, err)
	}

	now = now.Add(3 * time.Second)
	if !sender.Expired() {
		t.Fatalf("sender must expire after ttl")
	}
	if sent, err := sender.SendReady(context.Background(), peers); err != nil || sent != 0 {
		t.Fatalf("expired sender must not send, sent=%d err=%v", sent, err)
	}
}

func TestBroadcastFECBroadcasterSwitchesToShortRecovery(t *testing.T) {
	now := time.Unix(3000, 0)
	_, priv := keyPairFromSeed(54)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x43}, 32)}, BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(24),
		withBroadcastFECNow(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	peer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x04}, 32)}
	broadcaster, err := NewBroadcastFECBroadcaster(sender, mockBroadcastPeerSet{peers: []BroadcastPeer{peer}},
		WithBroadcastFECWorkerMinRate(16<<20),
		withBroadcastFECWorkerNow(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("broadcaster init failed: %v", err)
	}

	if err = broadcaster.Tick(context.Background()); err != nil {
		t.Fatalf("first tick failed: %v", err)
	}
	if len(peer.sent) == 0 {
		t.Fatalf("expected initial fast burst")
	}
	for i, msg := range peer.sent {
		if _, ok := msg.(*BroadcastFEC); !ok {
			t.Fatalf("expected full message at index %d, got %T", i, msg)
		}
	}

	if !broadcaster.TrackControlMessage(peer.id, FECReceived{Hash: sender.BroadcastHash()}) {
		t.Fatalf("expected received control to be tracked")
	}

	before := len(peer.sent)
	now = now.Add(10 * time.Millisecond)
	if err = broadcaster.Tick(context.Background()); err != nil {
		t.Fatalf("recovery tick failed: %v", err)
	}
	if len(peer.sent) <= before {
		t.Fatalf("expected recovery messages after control")
	}
	for _, msg := range peer.sent[before:] {
		if _, ok := msg.(*BroadcastFECShort); !ok {
			t.Fatalf("expected short recovery message, got %T", msg)
		}
	}
}

func TestBroadcastFECBroadcasterStopsOnCompleted(t *testing.T) {
	now := time.Unix(4000, 0)
	_, priv := keyPairFromSeed(55)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x44}, 32)}, BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(24),
		withBroadcastFECNow(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	peer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x05}, 32)}
	broadcaster, err := NewBroadcastFECBroadcaster(sender, mockBroadcastPeerSet{peers: []BroadcastPeer{peer}},
		WithBroadcastFECWorkerMinRate(16<<20),
		withBroadcastFECWorkerNow(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("broadcaster init failed: %v", err)
	}

	if err = broadcaster.Tick(context.Background()); err != nil {
		t.Fatalf("first tick failed: %v", err)
	}
	if !broadcaster.TrackControlMessage(peer.id, FECCompleted{Hash: sender.BroadcastHash()}) {
		t.Fatalf("expected completed control to be tracked")
	}

	before := len(peer.sent)
	now = now.Add(10 * time.Millisecond)
	if err = broadcaster.Tick(context.Background()); err != nil {
		t.Fatalf("post-complete tick failed: %v", err)
	}
	if len(peer.sent) != before {
		t.Fatalf("completed peer must not receive more messages")
	}
	if !broadcaster.Done() {
		t.Fatalf("single completed peer should finish broadcaster")
	}
}

func raptorQForTests(dataSize int, symbolSize uint32) any {
	return rldp.FECRaptorQ{
		DataSize:     uint32(dataSize),
		SymbolSize:   symbolSize,
		SymbolsCount: uint32((dataSize + int(symbolSize) - 1) / int(symbolSize)),
	}
}
