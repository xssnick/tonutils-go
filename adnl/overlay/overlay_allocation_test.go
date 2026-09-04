package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

type countingIDBroadcastPeer struct {
	*mockBroadcastPeer
	idCalls int
}

func (p *countingIDBroadcastPeer) ID() []byte {
	p.idCalls++
	return p.id
}

func TestBroadcastSimpleFixedEncodingMatchesTL(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x68}, ed25519.SeedSize))
	source := keys.PublicKeyED25519{Key: privateKey.Public().(ed25519.PublicKey)}
	dataHash := sha256.Sum256([]byte("simple broadcast"))

	for _, test := range []struct {
		name   string
		source any
		flags  int32
	}{
		{name: "source", source: source},
		{name: "source pointer", source: &source},
		{name: "any sender", source: struct{}{}, flags: BroadcastFlagAnySender},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := calcBroadcastIDFromDataHash(test.source, test.flags, dataHash[:])
			if err != nil {
				t.Fatal(err)
			}

			sourceID, err := broadcastSourceID(test.source, test.flags)
			if err != nil {
				t.Fatal(err)
			}
			want, err := tl.Hash(&BroadcastID{Source: sourceID[:], DataHash: dataHash[:], Flags: test.flags})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("fixed broadcast id %x differs from TL id %x", got, want)
			}
		})
	}

	if _, err := calcBroadcastIDFromDataHash(source, 0, dataHash[:len(dataHash)-1]); err == nil {
		t.Fatal("31-byte data hash was accepted")
	}
}

func TestBroadcastSimpleToSignFixedEncodingMatchesTL(t *testing.T) {
	hash := bytes.Repeat([]byte{0x75}, sha256.Size)
	got, err := serializeBroadcastToSign(hash, 0x12345678)
	if err != nil {
		t.Fatal(err)
	}
	want, err := tl.Serialize(&BroadcastToSign{Hash: hash, Date: 0x12345678}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fixed to-sign %x differs from TL encoding %x", got, want)
	}

	if _, err = serializeBroadcastToSign(hash[:len(hash)-1], 1); err == nil {
		t.Fatal("31-byte broadcast hash was accepted")
	}
}

func TestRelaySimpleBroadcastUsesPreparedAndLegacyPaths(t *testing.T) {
	message := &Broadcast{
		Source:      keys.PublicKeyED25519{Key: bytes.Repeat([]byte{0x31}, ed25519.PublicKeySize)},
		Certificate: CertificateEmpty{},
		Flags:       BroadcastFlagAnySender,
		Data:        []byte("payload"),
		Date:        1234,
		Signature:   bytes.Repeat([]byte{0x32}, ed25519.SignatureSize),
	}
	preparedPeer := &recordingPreparedBroadcastPeer{id: bytes.Repeat([]byte{0x41}, 32)}
	legacyPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x42}, 32)}
	state := NewBroadcastFECRelayState()
	receiver := &BroadcastReceiver{fecState: state}
	receiver.EnableBroadcastSimpleRelay(
		bytes.Repeat([]byte{0x43}, 32),
		StaticBroadcastPeerSet{preparedPeer, legacyPeer},
	)
	wrapper := &ADNLOverlayWrapper{BroadcastReceiver: receiver}

	if err := wrapper.relaySimpleBroadcast(context.Background(), nil, message); err != nil {
		t.Fatal(err)
	}

	legacyCalls, bodies := preparedPeer.snapshot()
	if legacyCalls != 0 || len(bodies) != 1 {
		t.Fatalf("prepared peer got %d legacy calls and %d bodies", legacyCalls, len(bodies))
	}
	wantBody, err := tl.Serialize(message, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bodies[0], wantBody) {
		t.Fatal("prepared peer got a non-canonical body")
	}
	if len(legacyPeer.sent) != 1 || legacyPeer.sent[0] != message {
		t.Fatalf("legacy peer got %#v", legacyPeer.sent)
	}
	if stats := state.Stats(); stats.SimpleRelaySentTotal != 2 || stats.SimpleRelayFailedTotal != 0 {
		t.Fatalf("unexpected relay stats: %#v", stats)
	}
}

func TestBroadcastFECPartCacheRingEvictsOldest(t *testing.T) {
	sender := &BroadcastFECSender{
		partCacheSize: 3,
		parts:         make(map[uint32]*broadcastFECSendPart, 3),
		partOrder:     make([]uint32, 0, 3),
	}
	parts := make([]*broadcastFECSendPart, 5)
	for seqno := range parts {
		parts[seqno] = &broadcastFECSendPart{}
		sender.cachePartLocked(uint32(seqno), parts[seqno])
	}

	if len(sender.parts) != 3 {
		t.Fatalf("cache contains %d parts, want 3", len(sender.parts))
	}
	for seqno := uint32(0); seqno < 2; seqno++ {
		if sender.parts[seqno] != nil {
			t.Fatalf("old part %d was retained", seqno)
		}
	}
	for seqno := uint32(2); seqno < 5; seqno++ {
		if sender.parts[seqno] != parts[seqno] {
			t.Fatalf("part %d was evicted", seqno)
		}
	}
}

func TestBroadcastFECEnsureWorkersReadsPeerIDOnce(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x46}, ed25519.SeedSize))
	sender, err := NewBroadcastFECSender(
		privateKey,
		CertificateEmpty{},
		[]byte("payload"),
		BroadcastFlagAnySender,
	)
	if err != nil {
		t.Fatal(err)
	}
	peer := &countingIDBroadcastPeer{mockBroadcastPeer: &mockBroadcastPeer{id: bytes.Repeat([]byte{0x47}, 32)}}
	broadcaster, err := NewBroadcastFECBroadcaster(sender, StaticBroadcastPeerSet{peer})
	if err != nil {
		t.Fatal(err)
	}

	broadcaster.ensureWorkers([]BroadcastPeer{peer})
	if peer.idCalls != 1 {
		t.Fatalf("new topology read peer ID %d times, want 1", peer.idCalls)
	}

	peer.idCalls = 0
	broadcaster.ensureWorkers([]BroadcastPeer{peer})
	if peer.idCalls != 1 {
		t.Fatalf("stable topology read peer ID %d times, want 1", peer.idCalls)
	}
}

func TestFECBroadcastStreamMapsAreLazy(t *testing.T) {
	state := NewBroadcastFECRelayState()
	id := testBroadcastFECIDKey([]byte("lazy stream"))
	stream, err := state.lockOrCreateFECStream(id, time.Now(), fecBroadcastStreamInit{
		fec:      rldp.FECRaptorQ{DataSize: 4, SymbolSize: 4, SymbolsCount: 1},
		partSize: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stream.partHashes != nil || stream.parts != nil || stream.receivedPeers != nil || stream.completedPeers != nil {
		stream.mx.Unlock()
		t.Fatal("new stream eagerly allocated sparse maps")
	}
	stream.mx.Unlock()

	peerID := []byte{1}
	if !state.TrackControlMessage(peerID, BroadcastFECControl{Hash: id[:]}) {
		t.Fatal("control with an arbitrary peer ID was rejected")
	}
	stream.mx.Lock()
	defer stream.mx.Unlock()
	peerKey := newBroadcastExternalPeerIDKey(peerID)
	if _, ok := stream.receivedPeers[peerKey]; !ok {
		t.Fatal("arbitrary peer ID was not retained")
	}
	if _, ok := stream.completedPeers[peerKey]; ok {
		t.Fatal("received control marked peer completed")
	}
}

func TestRLDPOverlayRegistryKeepsArbitraryIDCompatibility(t *testing.T) {
	wrapper := &RLDPWrapper{overlays: map[string]*RLDPOverlayWrapper{}}
	id := []byte{0x00, 0xFF, 0x01}

	first := wrapper.CreateOverlay(id)
	second := wrapper.CreateOverlay(bytes.Clone(id))
	if second != first {
		t.Fatal("equal arbitrary overlay IDs produced different wrappers")
	}
	if wrapper.overlays[string(id)] != first {
		t.Fatal("raw overlay ID was not registered")
	}

	first.closed.Store(true)
	replacement := wrapper.CreateOverlay(id)
	if replacement == first || wrapper.overlays[string(id)] != replacement {
		t.Fatal("closed arbitrary-ID overlay was not replaced")
	}
}

func BenchmarkBroadcastSimpleRelayFanoutSerialization(b *testing.B) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x6A}, ed25519.SeedSize))
	message := &Broadcast{
		Source:      ed25519Public(privateKey),
		Certificate: CertificateEmpty{},
		Flags:       BroadcastFlagAnySender,
		Data:        bytes.Repeat([]byte{0x44}, 300),
		Date:        1234,
		Signature:   bytes.Repeat([]byte{0x55}, ed25519.SignatureSize),
	}

	for _, prepared := range []bool{true, false} {
		name := "legacy"
		if prepared {
			name = "prepared"
		}

		b.Run(name, func(b *testing.B) {
			peers := make([]BroadcastPeer, 5)
			for i := range peers {
				legacy := benchmarkLegacyBroadcastPeer{id: bytes.Repeat([]byte{byte(i + 1)}, 32)}
				peers[i] = legacy
				if prepared {
					peers[i] = benchmarkPreparedBroadcastPeer{benchmarkLegacyBroadcastPeer: legacy}
				}
			}

			state := NewBroadcastFECRelayState()
			receiver := &BroadcastReceiver{fecState: state}
			receiver.EnableBroadcastSimpleRelay(bytes.Repeat([]byte{0x71}, 32), StaticBroadcastPeerSet(peers))
			wrapper := &ADNLOverlayWrapper{BroadcastReceiver: receiver}

			b.ReportAllocs()
			for b.Loop() {
				if err := wrapper.relaySimpleBroadcast(context.Background(), nil, message); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkBroadcastSimpleIDEncoding(b *testing.B) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x6B}, ed25519.SeedSize))
	source := keys.PublicKeyED25519{Key: privateKey.Public().(ed25519.PublicKey)}
	dataHash := sha256.Sum256([]byte("simple broadcast benchmark"))
	sourceID, err := broadcastSourceID(source, 0)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("fixed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkBroadcastSimpleHash, err = calcBroadcastIDFromDataHash(source, 0, dataHash[:])
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("reflective", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkBroadcastSimpleHash, err = tl.Hash(&BroadcastID{
				Source:   sourceID[:],
				DataHash: dataHash[:],
			})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkBroadcastFECEnsureWorkersStable(b *testing.B) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, ed25519.SeedSize))
	sender, err := NewBroadcastFECSender(
		privateKey,
		CertificateEmpty{},
		bytes.Repeat([]byte{0x73}, 128),
		BroadcastFlagAnySender,
	)
	if err != nil {
		b.Fatal(err)
	}

	peers := make([]BroadcastPeer, 15)
	for i := range peers {
		peers[i] = &mockBroadcastPeer{id: bytes.Repeat([]byte{byte(i + 1)}, 32)}
	}
	broadcaster, err := NewBroadcastFECBroadcaster(sender, StaticBroadcastPeerSet(peers))
	if err != nil {
		b.Fatal(err)
	}
	broadcaster.ensureWorkers(peers)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchmarkBroadcastFECWorkers = broadcaster.ensureWorkers(peers)
	}
}

func BenchmarkBroadcastFECPartCacheEviction(b *testing.B) {
	const cacheSize = 4096
	sender := &BroadcastFECSender{
		partCacheSize: cacheSize,
		parts:         make(map[uint32]*broadcastFECSendPart, cacheSize),
		partOrder:     make([]uint32, 0, cacheSize),
	}
	part := &broadcastFECSendPart{}
	for seqno := uint32(0); seqno < cacheSize; seqno++ {
		sender.cachePartLocked(seqno, part)
	}

	seqno := uint32(cacheSize)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sender.cachePartLocked(seqno, part)
		seqno++
	}
}

func BenchmarkBroadcastFECRegistryKeyInsert(b *testing.B) {
	var id [32]byte

	b.Run("fixed", func(b *testing.B) {
		registry := make(map[broadcastFECIDKey]struct{}, 1)
		b.ReportAllocs()
		for b.Loop() {
			key := broadcastFECIDKey(id)
			registry[key] = struct{}{}
			delete(registry, key)
			id[0]++
		}
	})
	b.Run("string", func(b *testing.B) {
		registry := make(map[string]struct{}, 1)
		b.ReportAllocs()
		for b.Loop() {
			key := string(id[:])
			registry[key] = struct{}{}
			delete(registry, key)
			id[0]++
		}
	})
}

func BenchmarkRLDPOverlayRegistryKey(b *testing.B) {
	id := bytes.Repeat([]byte{0x77}, 32)
	rawRegistry := map[string]struct{}{string(id): {}}
	hexRegistry := map[string]struct{}{hex.EncodeToString(id): {}}

	b.Run("raw", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, benchmarkRLDPOverlayFound = rawRegistry[string(id)]
		}
	})
	b.Run("hex", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, benchmarkRLDPOverlayFound = hexRegistry[hex.EncodeToString(id)]
		}
	})
}

var benchmarkBroadcastFECWorkers []*broadcastFECPeerWorker
var benchmarkBroadcastSimpleHash []byte
var benchmarkRLDPOverlayFound bool
