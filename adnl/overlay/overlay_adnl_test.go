package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

func signedBroadcastFEC(t *testing.T, priv ed25519.PrivateKey, fec any, data []byte, dataSize uint32, flags int32) *BroadcastFEC {
	t.Helper()

	dataHash := bytes.Repeat([]byte{0xCC}, 32)
	b := &BroadcastFEC{
		Source:   ed25519Public(priv),
		DataHash: dataHash,
		DataSize: dataSize,
		Flags:    flags,
		Data:     data,
		Seqno:    1,
		FEC:      fec,
		Date:     uint32(time.Now().Unix()),
	}

	if err := b.Sign(priv); err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	return b
}

func signedBroadcast(t *testing.T, priv ed25519.PrivateKey, data []byte, flags int32) *Broadcast {
	t.Helper()

	b := &Broadcast{
		Source:      ed25519Public(priv),
		Certificate: CertificateEmpty{},
		Flags:       flags,
		Data:        data,
		Date:        int32(time.Now().Unix()),
	}
	if err := b.Sign(priv); err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	return b
}

func ed25519Public(priv ed25519.PrivateKey) any {
	return keys.PublicKeyED25519{Key: priv.Public().(ed25519.PublicKey)}
}

func TestADNLOverlayCreateAndClose(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	overlayID := bytes.Repeat([]byte{0xAA}, 32)

	o1 := w.CreateOverlayWithSettings(overlayID, 7, false, true)
	o2 := w.CreateOverlayWithSettings(overlayID, 100, true, false)
	if o1 != o2 {
		t.Fatalf("expected existing overlay wrapper to be reused")
	}
	if o1.maxUnauthSize != 7 || o1.allowFEC || !o1.trustUnauthorized {
		t.Fatalf("unexpected overlay settings")
	}

	o1.Close()
	if len(w.overlays) != 0 {
		t.Fatalf("overlay must be removed after close")
	}
}

func TestADNLOverlaySetAuthorizedKeysCopiesInput(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.WithOverlay(bytes.Repeat([]byte{0xAB}, 32))

	input := map[string]uint32{"a": 11}
	o.SetAuthorizedKeys(input)

	input["a"] = 1
	input["b"] = 2

	if o.authorizedKeys["a"] != 11 {
		t.Fatalf("expected key map copy to preserve original values")
	}
	if _, ok := o.authorizedKeys["b"]; ok {
		t.Fatalf("unexpected copied key from mutated input")
	}
}

func TestADNLOverlayCheckRules(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x10}, 32), 10, false, false)
	o.SetAuthorizedKeys(map[string]uint32{"auth": 5, "tiny": 1})

	if res := o.checkRules("any", 0, false); res != CertCheckResultForbidden {
		t.Fatalf("expected forbidden for zero size")
	}
	if res := o.checkRules("auth", 5, false); res != CertCheckResultTrusted {
		t.Fatalf("expected trusted for authorized key")
	}
	if res := o.checkRules("tiny", 5, false); res != CertCheckResultForbidden {
		t.Fatalf("expected forbidden for oversized authorized payload")
	}
	if res := o.checkRules("unknown", 11, false); res != CertCheckResultForbidden {
		t.Fatalf("expected forbidden for oversized unauthorized payload")
	}
	if res := o.checkRules("unknown", 5, true); res != CertCheckResultForbidden {
		t.Fatalf("expected forbidden for unauthorized fec")
	}
	if res := o.checkRules("unknown", 5, false); res != CertCheckResultNeedCheck {
		t.Fatalf("expected need-check for allowed unauthorized payload")
	}

	o.trustUnauthorized = true
	if res := o.checkRules("unknown", 5, false); res != CertCheckResultTrusted {
		t.Fatalf("expected trusted for trustUnauthorized")
	}
}

func TestProcessBroadcastSimpleDeliversDedupsAndRelays(t *testing.T) {
	m := newMockADNL()
	sourcePeerID := bytes.Repeat([]byte{0x21}, 32)
	m.id = sourcePeerID
	w := CreateExtendedADNL(m)
	overlayID := bytes.Repeat([]byte{0x22}, 32)
	o := w.CreateOverlayWithSettings(overlayID, 4096, true, true)

	localPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x23}, 32)}
	relayPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0x24}, 32)}
	state := NewBroadcastFECRelayState()
	o.EnableBroadcastFECRelay(localPeer.id, mockBroadcastPeerSet{peers: []BroadcastPeer{
		&mockBroadcastPeer{id: sourcePeerID},
		localPeer,
		relayPeer,
	}}, state)

	_, priv := keyPairFromSeed(30)
	payloadOverlay := bytes.Repeat([]byte{0x25}, 32)
	payload, err := tl.Serialize(Message{Overlay: payloadOverlay}, true)
	if err != nil {
		t.Fatalf("payload serialize failed: %v", err)
	}
	msg := signedBroadcast(t, priv, payload, BroadcastFlagAnySender)
	broadcastID, err := msg.CalcID()
	if err != nil {
		t.Fatalf("calc id failed: %v", err)
	}

	handled := 0
	var gotInfo BroadcastInfo
	o.SetBroadcastHandlerWithInfo(func(got tl.Serializable, info BroadcastInfo) error {
		handled++
		gotMsg, ok := got.(Message)
		if !ok {
			t.Fatalf("unexpected decoded payload type %T", got)
		}
		if !bytes.Equal(gotMsg.Overlay, payloadOverlay) {
			t.Fatalf("unexpected decoded payload overlay")
		}
		gotInfo = info
		return nil
	})

	if err = o.processBroadcast(msg, sourcePeerID); err != nil {
		t.Fatalf("process simple broadcast failed: %v", err)
	}
	if handled != 1 {
		t.Fatalf("expected one delivery, got %d", handled)
	}
	if gotInfo.TwoStep || !gotInfo.Trusted || !bytes.Equal(gotInfo.BroadcastID, broadcastID) {
		t.Fatalf("unexpected broadcast info: %#v", gotInfo)
	}
	if len(relayPeer.sent) != 1 {
		t.Fatalf("expected one simple relay, got %d", len(relayPeer.sent))
	}
	if _, ok := relayPeer.sent[0].(*Broadcast); !ok {
		t.Fatalf("expected broadcast relay, got %T", relayPeer.sent[0])
	}
	if len(localPeer.sent) != 0 {
		t.Fatalf("local peer must be skipped")
	}
	if stats := state.Stats(); stats.SimpleRelaySentTotal != 1 || stats.SimpleRelayFailedTotal != 0 || stats.FECRelaySentTotal != 0 {
		t.Fatalf("unexpected simple relay stats: %#v", stats)
	}

	dup := *msg
	dup.Signature = []byte("bad signature")
	if err = o.processBroadcast(&dup, sourcePeerID); err != nil {
		t.Fatalf("duplicate simple broadcast should be dropped before signature check: %v", err)
	}
	if handled != 1 || len(relayPeer.sent) != 1 {
		t.Fatalf("duplicate should not deliver or relay again, deliveries=%d relays=%d", handled, len(relayPeer.sent))
	}
}

func TestADNLOverlaySendQueryAndRandomPeers(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	overlayID := bytes.Repeat([]byte{0xF1}, 32)
	o := w.WithOverlay(overlayID)

	m.queryResponder = func(req tl.Serializable, result tl.Serializable) error {
		arr, ok := req.([]tl.Serializable)
		if !ok || len(arr) != 2 {
			return errors.New("invalid wrapped query")
		}
		q, ok := arr[0].(Query)
		if !ok || !bytes.Equal(q.Overlay, overlayID) {
			return errors.New("invalid overlay id")
		}
		if _, ok = arr[1].(GetRandomPeers); !ok {
			return errors.New("invalid payload type")
		}
		return setSerializableResult(result, NodesList{List: []Node{{Version: 77}}})
	}

	if err := o.SendCustomMessage(context.Background(), tl.Raw([]byte{1})); err != nil {
		t.Fatalf("send custom message failed: %v", err)
	}
	if len(m.sendCustomCalls) != 1 {
		t.Fatalf("expected one custom message call")
	}
	wrapped, ok := m.sendCustomCalls[0].([]tl.Serializable)
	if !ok || len(wrapped) != 2 {
		t.Fatalf("expected wrapped custom message")
	}
	if msg, ok := wrapped[0].(Message); !ok || !bytes.Equal(msg.Overlay, overlayID) {
		t.Fatalf("unexpected wrapped message header")
	}

	peers, err := o.GetRandomPeers(context.Background())
	if err != nil {
		t.Fatalf("get random peers failed: %v", err)
	}
	if len(peers) != 1 || peers[0].Version != 77 {
		t.Fatalf("unexpected peers result: %#v", peers)
	}
}

func TestProcessFECBroadcast_ErrorAndFinishedFlow(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x44}, 32), 64, true, true)

	invalidSigner := &BroadcastFEC{
		Source:   keys.PublicKeyAES{Key: bytes.Repeat([]byte{0x11}, 32)},
		DataHash: bytes.Repeat([]byte{0x10}, 32),
		FEC:      rldp.FECRaptorQ{DataSize: 4, SymbolSize: 2, SymbolsCount: 2},
		DataSize: 4,
		Data:     []byte{1, 2, 3, 4},
		Seqno:    1,
		Date:     uint32(time.Now().Unix()),
	}
	if err := o.processFECBroadcast(invalidSigner); err == nil || !strings.Contains(err.Error(), "invalid signer key format") {
		t.Fatalf("expected invalid signer format error, got: %v", err)
	}

	_, priv := keyPairFromSeed(31)
	unsupportedFEC := signedBroadcastFEC(t, priv, rldp.FECRoundRobin{DataSize: 4, SymbolSize: 2, SymbolsCount: 2}, []byte{1, 2, 3, 4}, 4, 0)
	if err := o.processFECBroadcast(unsupportedFEC); err == nil || !strings.Contains(err.Error(), "not supported fec type") {
		t.Fatalf("expected unsupported fec type error, got: %v", err)
	}

	wrongSize := signedBroadcastFEC(t, priv, rldp.FECRaptorQ{DataSize: 8, SymbolSize: 2, SymbolsCount: 4}, []byte{1, 2, 3, 4}, 4, 0)
	if err := o.processFECBroadcast(wrongSize); err == nil || !strings.Contains(err.Error(), "incorrect data size") {
		t.Fatalf("expected data size mismatch error, got: %v", err)
	}

	invalidFECTests := []struct {
		name string
		fec  rldp.FECRaptorQ
		err  string
	}{
		{name: "zero symbol size", fec: rldp.FECRaptorQ{DataSize: 4, SymbolSize: 0, SymbolsCount: 2}, err: "symbol_size is 0"},
		{name: "large symbol size", fec: rldp.FECRaptorQ{DataSize: 4, SymbolSize: maxFECBroadcastSymbolSize + 1, SymbolsCount: 1}, err: "symbol_size is too big"},
		{name: "wrong symbols count", fec: rldp.FECRaptorQ{DataSize: 4, SymbolSize: 2, SymbolsCount: 3}, err: "wrong symbols_count"},
	}
	for _, tt := range invalidFECTests {
		invalidFEC := signedBroadcastFEC(t, priv, tt.fec, []byte{1, 2, 3, 4}, 4, 0)
		if err := o.processFECBroadcast(invalidFEC); err == nil || !strings.Contains(err.Error(), tt.err) {
			t.Fatalf("expected invalid fec error %q for %s, got: %v", tt.err, tt.name, err)
		}
		if stats := o.FECBroadcastStats(); stats.DroppedTotal != 0 {
			t.Fatalf("invalid fec must not count as budget drop: %#v", stats)
		}
	}

	finished := signedBroadcastFEC(t, priv, rldp.FECRaptorQ{DataSize: 4, SymbolSize: 2, SymbolsCount: 2}, []byte{1, 2, 3, 4}, 4, BroadcastFlagAnySender)
	id, err := finished.CalcID()
	if err != nil {
		t.Fatalf("calc id failed: %v", err)
	}
	now := time.Now()
	state := o.activeFECState()
	state.streams[string(id)] = &fecBroadcastStream{
		source:        priv.Public().(ed25519.PublicKey),
		fec:           finished.FEC.(rldp.FECRaptorQ),
		date:          finished.Date,
		flags:         finished.Flags,
		finishedAt:    &now,
		completedAt:   nil,
		lastMessageAt: now,
	}

	if err = o.processFECBroadcast(finished); err != nil {
		t.Fatalf("expected finished stream ack path, got err=%v", err)
	}
	if len(m.sendCustomCalls) == 0 {
		t.Fatalf("expected FEC ack to be sent")
	}
	if _, ok := m.sendCustomCalls[len(m.sendCustomCalls)-1].(FECReceived); !ok {
		t.Fatalf("expected FECReceived while handler is not completed")
	}

	state.streams[string(id)].completedAt = &now
	if err = o.processFECBroadcast(finished); err != nil {
		t.Fatalf("expected completed stream ack path, got err=%v", err)
	}
	if _, ok := m.sendCustomCalls[len(m.sendCustomCalls)-1].(FECCompleted); !ok {
		t.Fatalf("expected FECCompleted after handler completion")
	}

	otherPub, otherPriv := keyPairFromSeed(32)
	_ = otherPub
	mismatch := signedBroadcastFEC(t, otherPriv, finished.FEC, finished.Data, finished.DataSize, finished.Flags)
	mismatch.Seqno = finished.Seqno
	mismatch.Date = finished.Date
	if err = o.processFECBroadcast(mismatch); err != nil {
		t.Fatalf("any-sender finished stream should accept another source for ack path: %v", err)
	}
}

func TestProcessFECBroadcastDropsDuplicateBeforeSignature(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x4A}, 32), 4096, true, true)

	_, priv := keyPairFromSeed(42)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x4B}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(8))
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	part, err := sender.part(0)
	if err != nil {
		t.Fatalf("part build failed: %v", err)
	}
	if err = o.processFECBroadcast(part.full); err != nil {
		t.Fatalf("process first part failed: %v", err)
	}

	duplicate := *part.full
	duplicate.Signature = []byte("bad signature")
	if err = o.processFECBroadcast(&duplicate); err != nil {
		t.Fatalf("duplicate part should be dropped before signature check: %v", err)
	}
}

func TestProcessFECBroadcastDeliveredCacheBeforeSignature(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x4C}, 32), 4096, true, true)

	_, priv := keyPairFromSeed(43)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x4D}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(256))
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	part, err := sender.part(0)
	if err != nil {
		t.Fatalf("part build failed: %v", err)
	}
	if err = o.processFECBroadcast(part.full); err != nil {
		t.Fatalf("process complete part failed: %v", err)
	}

	late := *part.full
	late.Signature = []byte("bad signature")
	if err = o.processFECBroadcast(&late); err != nil {
		t.Fatalf("late delivered part should hit cache before signature check: %v", err)
	}
	if _, ok := m.sendCustomCalls[len(m.sendCustomCalls)-1].(FECCompleted); !ok {
		t.Fatalf("expected completed ack for delivered duplicate, got %T", m.sendCustomCalls[len(m.sendCustomCalls)-1])
	}
}

func TestProcessFECBroadcastShort_KnownAndFinishedState(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x54}, 32), 4096, true, true)

	_, priv := keyPairFromSeed(33)
	expectedOverlay := bytes.Repeat([]byte{0x29}, 32)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: expectedOverlay}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(256))
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	handled := 0
	o.SetBroadcastHandler(func(msg tl.Serializable, trusted bool) error {
		handled++
		got, ok := msg.(Message)
		if !ok {
			t.Fatalf("unexpected decoded payload type %T", msg)
		}
		if !bytes.Equal(got.Overlay, expectedOverlay) {
			t.Fatalf("unexpected decoded payload")
		}
		return nil
	})

	part0, err := sender.part(0)
	if err != nil {
		t.Fatalf("part 0 build failed: %v", err)
	}
	if err = o.processFECBroadcast(part0.full); err != nil {
		t.Fatalf("process full part 0 failed: %v", err)
	}

	if handled != 1 {
		t.Fatalf("expected one decoded broadcast, got %d", handled)
	}
	if len(m.sendCustomCalls) < 2 {
		t.Fatalf("expected received/completed acks after decode")
	}
	if _, ok := m.sendCustomCalls[len(m.sendCustomCalls)-2].(FECReceived); !ok {
		t.Fatalf("expected FECReceived before completion ack, got %T", m.sendCustomCalls[len(m.sendCustomCalls)-2])
	}
	if _, ok := m.sendCustomCalls[len(m.sendCustomCalls)-1].(FECCompleted); !ok {
		t.Fatalf("expected FECCompleted after decode, got %T", m.sendCustomCalls[len(m.sendCustomCalls)-1])
	}

	state := o.activeFECState()
	state.mx.RLock()
	stream := state.streams[string(sender.BroadcastHash())]
	state.mx.RUnlock()
	if stream != nil {
		t.Fatalf("completed broadcast stream should be removed after decode")
	}
	stats := o.FECBroadcastStats()
	if stats.ActiveStreams != 0 || stats.ActiveBytes != 0 {
		t.Fatalf("expected no active FEC state after decode, got %#v", stats)
	}
	if stats.DeliveredBroadcasts != 1 || stats.CompletedTotal != 1 {
		t.Fatalf("expected delivered broadcast to be cached, got %#v", stats)
	}

	shortPart, err := sender.part(sender.fec.SymbolsCount)
	if err != nil {
		t.Fatalf("repair part build failed: %v", err)
	}
	if err = o.processFECBroadcastShort(shortPart.short); err != nil {
		t.Fatalf("finished short part processing failed: %v", err)
	}
	if _, ok := m.sendCustomCalls[len(m.sendCustomCalls)-1].(FECCompleted); !ok {
		t.Fatalf("expected FECCompleted on finished short part, got %T", m.sendCustomCalls[len(m.sendCustomCalls)-1])
	}
	if stats = o.FECBroadcastStats(); stats.DeliveredCacheHitsTotal != 1 {
		t.Fatalf("expected delivered cache hit for late short part, got %#v", stats)
	}

	if err = o.processFECBroadcast(part0.full); err != nil {
		t.Fatalf("late full part should hit delivered cache: %v", err)
	}
	if handled != 1 {
		t.Fatalf("late full part must not deliver broadcast again, got %d deliveries", handled)
	}
	if _, ok := m.sendCustomCalls[len(m.sendCustomCalls)-1].(FECCompleted); !ok {
		t.Fatalf("expected FECCompleted on delivered full part, got %T", m.sendCustomCalls[len(m.sendCustomCalls)-1])
	}
	if stats = o.FECBroadcastStats(); stats.ActiveStreams != 0 || stats.DeliveredCacheHitsTotal != 2 {
		t.Fatalf("late full part should not recreate active stream, got %#v", stats)
	}
}

func TestProcessFECBroadcastDeliveredCacheExpires(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x55}, 32), 4096, true, true)

	_, priv := keyPairFromSeed(34)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x56}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(256))
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	part0, err := sender.part(0)
	if err != nil {
		t.Fatalf("part 0 build failed: %v", err)
	}
	if err = o.processFECBroadcast(part0.full); err != nil {
		t.Fatalf("process full part 0 failed: %v", err)
	}
	if stats := o.FECBroadcastStats(); stats.DeliveredBroadcasts != 1 {
		t.Fatalf("expected delivered broadcast to be cached, got %#v", stats)
	}

	state := o.activeFECState()
	state.mx.Lock()
	state.cleanupLocked(time.Now().Add(broadcastFECDeliveredTTL+time.Second), true)
	delivered := len(state.delivered)
	state.mx.Unlock()
	if delivered != 0 {
		t.Fatalf("expected delivered cache to expire, got %d entries", delivered)
	}

	shortPart, err := sender.part(sender.fec.SymbolsCount)
	if err != nil {
		t.Fatalf("repair part build failed: %v", err)
	}
	if err = o.processFECBroadcastShort(shortPart.short); err == nil || !strings.Contains(err.Error(), "unknown broadcast") {
		t.Fatalf("expected expired delivered cache to miss, got: %v", err)
	}
	if stats := o.FECBroadcastStats(); stats.DeliveredBroadcasts != 0 || stats.DeliveredCacheHitsTotal != 0 {
		t.Fatalf("expired delivered cache should not count as a hit, got %#v", stats)
	}
}

func TestProcessFECBroadcastDeliversWhenReceivedAckFails(t *testing.T) {
	m := newMockADNL()
	m.sendCustomErr = errors.New("ack send failed")
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x74}, 32), 4096, true, true)

	_, priv := keyPairFromSeed(40)
	expectedOverlay := bytes.Repeat([]byte{0x75}, 32)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: expectedOverlay}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(256))
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	handled := 0
	o.SetBroadcastHandler(func(msg tl.Serializable, trusted bool) error {
		handled++
		got, ok := msg.(Message)
		if !ok {
			t.Fatalf("unexpected decoded payload type %T", msg)
		}
		if !bytes.Equal(got.Overlay, expectedOverlay) {
			t.Fatalf("unexpected decoded payload")
		}
		return nil
	})

	part0, err := sender.part(0)
	if err != nil {
		t.Fatalf("part 0 build failed: %v", err)
	}
	err = o.processFECBroadcast(part0.full)
	if err == nil || !strings.Contains(err.Error(), "ack send failed") {
		t.Fatalf("expected ack send error, got %v", err)
	}
	if handled != 1 {
		t.Fatalf("decoded broadcast must be delivered despite ack error, got %d deliveries", handled)
	}
}

func TestProcessFECBroadcastShortFinishedButNotCompleted(t *testing.T) {
	m := newMockADNL()
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x65}, 32), 4096, true, true)

	_, priv := keyPairFromSeed(38)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x3A}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(24))
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	part0, err := sender.part(0)
	if err != nil {
		t.Fatalf("part 0 build failed: %v", err)
	}

	now := time.Now()
	state := o.activeFECState()
	state.streams[string(sender.BroadcastHash())] = &fecBroadcastStream{
		source:        priv.Public().(ed25519.PublicKey),
		fec:           sender.fec,
		encoder:       sender.encoder,
		date:          part0.full.Date,
		flags:         part0.full.Flags,
		finishedAt:    &now,
		lastMessageAt: now,
	}

	if err = o.processFECBroadcastShort(part0.short); err != nil {
		t.Fatalf("finished short part processing failed: %v", err)
	}
	if _, ok := m.sendCustomCalls[len(m.sendCustomCalls)-1].(FECReceived); !ok {
		t.Fatalf("expected FECReceived while handler is not completed, got %T", m.sendCustomCalls[len(m.sendCustomCalls)-1])
	}

	state.streams[string(sender.BroadcastHash())].completedAt = &now
	repairPart, err := sender.part(sender.fec.SymbolsCount)
	if err != nil {
		t.Fatalf("repair part build failed: %v", err)
	}
	if err = o.processFECBroadcastShort(repairPart.short); err != nil {
		t.Fatalf("completed short part processing failed: %v", err)
	}
	if _, ok := m.sendCustomCalls[len(m.sendCustomCalls)-1].(FECCompleted); !ok {
		t.Fatalf("expected FECCompleted after handler completion, got %T", m.sendCustomCalls[len(m.sendCustomCalls)-1])
	}
}

func TestProcessFECBroadcastHandlerWithInfo(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	overlayID := bytes.Repeat([]byte{0x55}, 32)
	o := w.CreateOverlayWithSettings(overlayID, 4096, true, true)

	_, priv := keyPairFromSeed(35)
	payloadOverlay := bytes.Repeat([]byte{0x2A}, 32)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: payloadOverlay}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(256))
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	sourceID, err := tl.Hash(ed25519Public(priv))
	if err != nil {
		t.Fatalf("source id hash failed: %v", err)
	}

	oldHandlerCalled := false
	var gotInfo BroadcastInfo
	o.SetBroadcastHandler(func(msg tl.Serializable, trusted bool) error {
		oldHandlerCalled = true
		return nil
	})
	o.SetBroadcastHandlerWithInfo(func(msg tl.Serializable, info BroadcastInfo) error {
		got, ok := msg.(Message)
		if !ok {
			t.Fatalf("unexpected decoded payload type %T", msg)
		}
		if !bytes.Equal(got.Overlay, payloadOverlay) {
			t.Fatalf("unexpected decoded payload overlay")
		}
		gotInfo = info
		return nil
	})

	part0, err := sender.part(0)
	if err != nil {
		t.Fatalf("part 0 build failed: %v", err)
	}
	if err = o.processFECBroadcast(part0.full); err != nil {
		t.Fatalf("process full part 0 failed: %v", err)
	}

	if oldHandlerCalled {
		t.Fatalf("legacy handler should not be called when handler with info is set")
	}
	if !bytes.Equal(gotInfo.SourceID, sourceID) {
		t.Fatalf("unexpected source id: %x want %x", gotInfo.SourceID, sourceID)
	}
	if !bytes.Equal(gotInfo.SourceKey, priv.Public().(ed25519.PublicKey)) {
		t.Fatalf("unexpected source key")
	}
	if !gotInfo.Trusted {
		t.Fatalf("expected trusted broadcast info")
	}
	if !bytes.Equal(gotInfo.OverlayID, overlayID) {
		t.Fatalf("unexpected overlay id: %x want %x", gotInfo.OverlayID, overlayID)
	}
}

func TestProcessFECBroadcastShort_KnownPartialState(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x64}, 32), 4096, true, true)

	_, priv := keyPairFromSeed(34)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x39}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(24))
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	part0, err := sender.part(0)
	if err != nil {
		t.Fatalf("part 0 build failed: %v", err)
	}
	if err = o.processFECBroadcast(part0.full); err != nil {
		t.Fatalf("process first full part failed: %v", err)
	}
	if err = o.processFECBroadcastShort(part0.short); err == nil || !strings.Contains(err.Error(), "unfinished broadcast") {
		t.Fatalf("expected unfinished broadcast error for partial stream short part, got: %v", err)
	}

	part1, err := sender.part(1)
	if err != nil {
		t.Fatalf("part 1 build failed: %v", err)
	}
	if err = o.processFECBroadcastShort(part1.short); err == nil || !strings.Contains(err.Error(), "unfinished broadcast") {
		t.Fatalf("expected unfinished broadcast error for unseen short part, got: %v", err)
	}
}

func TestProcessFECBroadcastDropsWhenBudgetTooSmall(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x74}, 32), 4096, true, true)
	o.SetFECBroadcastLimits(8, 1)

	_, priv := keyPairFromSeed(36)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x49}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(24))
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	part0, err := sender.part(0)
	if err != nil {
		t.Fatalf("part 0 build failed: %v", err)
	}
	if err = o.processFECBroadcast(part0.full); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected budget error, got: %v", err)
	}

	stats := o.FECBroadcastStats()
	if stats.ActiveStreams != 0 || stats.ActiveBytes != 0 || stats.DroppedTotal != 1 {
		t.Fatalf("unexpected budget stats: %#v", stats)
	}
}

func TestProcessFECBroadcastDropsNewStreamWhenLimitReached(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x84}, 32), 4096, true, true)
	o.SetFECBroadcastLimits(1, 1<<20)

	_, firstPriv := keyPairFromSeed(37)
	firstSender, err := NewBroadcastFECSenderFromTL(firstPriv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x59}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(24))
	if err != nil {
		t.Fatalf("first sender init failed: %v", err)
	}
	firstPart, err := firstSender.part(0)
	if err != nil {
		t.Fatalf("first part build failed: %v", err)
	}
	if err = o.processFECBroadcast(firstPart.full); err != nil {
		t.Fatalf("first partial broadcast failed: %v", err)
	}

	_, secondPriv := keyPairFromSeed(38)
	secondSender, err := NewBroadcastFECSenderFromTL(secondPriv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x5A}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(24))
	if err != nil {
		t.Fatalf("second sender init failed: %v", err)
	}
	secondPart, err := secondSender.part(0)
	if err != nil {
		t.Fatalf("second part build failed: %v", err)
	}
	if err = o.processFECBroadcast(secondPart.full); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected second partial broadcast to hit budget, got: %v", err)
	}

	firstHash := string(firstSender.BroadcastHash())
	secondHash := string(secondSender.BroadcastHash())

	state := o.activeFECState()
	state.mx.RLock()
	firstStream := state.streams[firstHash]
	secondStream := state.streams[secondHash]
	state.mx.RUnlock()

	if firstStream == nil {
		t.Fatalf("expected first stream to stay active")
	}
	if secondStream != nil {
		t.Fatalf("expected second stream to be dropped")
	}

	stats := o.FECBroadcastStats()
	if stats.ActiveStreams != 1 || stats.EvictedTotal != 0 || stats.DroppedTotal != 1 {
		t.Fatalf("unexpected budget stats: %#v", stats)
	}
}

func TestProcessFECBroadcastCleanupEvictsStaleStream(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0x94}, 32), 4096, true, true)

	_, priv := keyPairFromSeed(39)
	sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0x69}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(24))
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	part0, err := sender.part(0)
	if err != nil {
		t.Fatalf("part 0 build failed: %v", err)
	}
	if err = o.processFECBroadcast(part0.full); err != nil {
		t.Fatalf("partial broadcast failed: %v", err)
	}

	hash := string(sender.BroadcastHash())
	state := o.activeFECState()
	state.mx.Lock()
	stream := state.streams[hash]
	if stream == nil {
		state.mx.Unlock()
		t.Fatalf("expected active stream")
	}
	stream.lastMessageAt = time.Now().Add(-fecBroadcastStreamIdleTTL - time.Second)
	state.cleanupLocked(time.Now(), true)
	state.mx.Unlock()

	stats := o.FECBroadcastStats()
	if stats.ActiveStreams != 0 || stats.DeliveredBroadcasts != 0 || stats.EvictedTotal != 1 {
		t.Fatalf("unexpected cleanup stats: %#v", stats)
	}
}

func TestBroadcastFECRelayStreamsTrustedParts(t *testing.T) {
	m := newMockADNL()
	sourcePeerID := bytes.Repeat([]byte{0xA1}, 32)
	m.id = sourcePeerID
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0xA2}, 32), 4096, true, true)

	localPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0xA3}, 32)}
	receivedPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0xA4}, 32)}
	fullPeer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0xA5}, 32)}
	state := NewBroadcastFECRelayState()
	o.EnableBroadcastFECRelay(localPeer.id, mockBroadcastPeerSet{peers: []BroadcastPeer{
		&mockBroadcastPeer{id: sourcePeerID},
		localPeer,
		receivedPeer,
		fullPeer,
	}}, state)

	_, priv := keyPairFromSeed(71)
	payload, err := tl.Serialize(Message{Overlay: bytes.Repeat([]byte{0xA6}, 32)}, true)
	if err != nil {
		t.Fatalf("payload serialize failed: %v", err)
	}
	sender, err := NewBroadcastFECSender(
		priv,
		CertificateEmpty{},
		payload,
		BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(8),
	)
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	part0, err := sender.part(0)
	if err != nil {
		t.Fatalf("part 0 build failed: %v", err)
	}
	if err = o.processFECBroadcast(part0.full); err != nil {
		t.Fatalf("process first full part failed: %v", err)
	}
	if len(fullPeer.sent) != 1 {
		t.Fatalf("expected full peer to receive first relayed part, got %d", len(fullPeer.sent))
	}
	if _, ok := fullPeer.sent[0].(*BroadcastFEC); !ok {
		t.Fatalf("expected full FEC relay, got %T", fullPeer.sent[0])
	}
	if len(receivedPeer.sent) != 1 {
		t.Fatalf("expected received peer to receive first full part before control, got %d", len(receivedPeer.sent))
	}
	if len(localPeer.sent) != 0 {
		t.Fatalf("local peer must be skipped")
	}
	if stats := state.Stats(); stats.FECRelaySentTotal != 2 || stats.FECRelayFailedTotal != 0 || stats.SimpleRelaySentTotal != 0 {
		t.Fatalf("unexpected FEC relay stats after first part: %#v", stats)
	}

	if !state.TrackControlMessage(receivedPeer.id, BroadcastFECControl{Hash: sender.BroadcastHash()}) {
		t.Fatalf("expected relay state to track received control")
	}

	part1, err := sender.part(1)
	if err != nil {
		t.Fatalf("part 1 build failed: %v", err)
	}
	if err = o.processFECBroadcast(part1.full); err != nil {
		t.Fatalf("process second full part failed: %v", err)
	}
	if len(receivedPeer.sent) != 2 {
		t.Fatalf("expected received peer to receive second relay, got %d", len(receivedPeer.sent))
	}
	if _, ok := receivedPeer.sent[1].(*BroadcastFECShort); !ok {
		t.Fatalf("expected short FEC relay after received control, got %T", receivedPeer.sent[1])
	}
	if _, ok := fullPeer.sent[1].(*BroadcastFEC); !ok {
		t.Fatalf("expected full FEC relay for peer without received control, got %T", fullPeer.sent[1])
	}

	for seqno := uint32(2); seqno < sender.fec.SymbolsCount; seqno++ {
		part, err := sender.part(seqno)
		if err != nil {
			t.Fatalf("part %d build failed: %v", seqno, err)
		}
		if err = o.processFECBroadcast(part.full); err != nil {
			t.Fatalf("process full part %d failed: %v", seqno, err)
		}
	}

	state.mx.RLock()
	retained := state.streams[string(sender.BroadcastHash())]
	state.mx.RUnlock()
	if retained == nil {
		t.Fatalf("completed relay stream should be retained for short parts")
	}
	retained.mx.Lock()
	hasEncoder := retained.encoder != nil
	retained.mx.Unlock()
	if !hasEncoder {
		t.Fatalf("completed relay stream should keep encoder for short parts")
	}

	beforeFull := len(fullPeer.sent)
	beforeReceived := len(receivedPeer.sent)
	shortRepairPart, err := sender.part(sender.fec.SymbolsCount)
	if err != nil {
		t.Fatalf("short repair part build failed: %v", err)
	}
	if err = o.processFECBroadcastShort(shortRepairPart.short); err != nil {
		t.Fatalf("process post-completion short part failed: %v", err)
	}
	if len(fullPeer.sent) != beforeFull+1 {
		t.Fatalf("expected short part to relay reconstructed full part, got %d sends before and %d after", beforeFull, len(fullPeer.sent))
	}
	if _, ok := fullPeer.sent[len(fullPeer.sent)-1].(*BroadcastFEC); !ok {
		t.Fatalf("expected reconstructed full FEC relay, got %T", fullPeer.sent[len(fullPeer.sent)-1])
	}
	if len(receivedPeer.sent) != beforeReceived+1 {
		t.Fatalf("expected received peer to get short relay, got %d sends before and %d after", beforeReceived, len(receivedPeer.sent))
	}
	if _, ok := receivedPeer.sent[len(receivedPeer.sent)-1].(*BroadcastFECShort); !ok {
		t.Fatalf("expected short FEC relay, got %T", receivedPeer.sent[len(receivedPeer.sent)-1])
	}

	before := len(fullPeer.sent)
	repairPart, err := sender.part(sender.fec.SymbolsCount + 1)
	if err != nil {
		t.Fatalf("repair part build failed: %v", err)
	}
	if err = o.processFECBroadcast(repairPart.full); err != nil {
		t.Fatalf("process post-completion repair part failed: %v", err)
	}
	if len(fullPeer.sent) != before+1 {
		t.Fatalf("expected post-completion full part relay, got %d sends before and %d after", before, len(fullPeer.sent))
	}
	if _, ok := fullPeer.sent[len(fullPeer.sent)-1].(*BroadcastFEC); !ok {
		t.Fatalf("expected post-completion full FEC relay, got %T", fullPeer.sent[len(fullPeer.sent)-1])
	}
}

func TestBroadcastFECRelayWaitsForUntrustedCheck(t *testing.T) {
	t.Run("reject", func(t *testing.T) {
		w := CreateExtendedADNL(newMockADNL())
		o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0xB1}, 32), 4096, true, false)
		peer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0xB2}, 32)}
		o.EnableBroadcastFECRelay(bytes.Repeat([]byte{0xB3}, 32), mockBroadcastPeerSet{peers: []BroadcastPeer{peer}}, NewBroadcastFECRelayState())
		o.SetBroadcastCheckHandler(func(msg tl.Serializable, info BroadcastInfo) error {
			return errors.New("reject")
		})

		_, priv := keyPairFromSeed(72)
		sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0xB4}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(256))
		if err != nil {
			t.Fatalf("sender init failed: %v", err)
		}
		part, err := sender.part(0)
		if err != nil {
			t.Fatalf("part build failed: %v", err)
		}

		err = o.processFECBroadcast(part.full)
		if err == nil || !strings.Contains(err.Error(), "reject") {
			t.Fatalf("expected check error, got %v", err)
		}
		if len(peer.sent) != 0 {
			t.Fatalf("untrusted rejected broadcast must not be relayed, got %d sends", len(peer.sent))
		}
		if stats := o.FECBroadcastStats(); stats.ActiveStreams != 0 || stats.ActiveBytes != 0 {
			t.Fatalf("rejected untrusted broadcast must release FEC state, got %#v", stats)
		}
	})

	t.Run("accept", func(t *testing.T) {
		w := CreateExtendedADNL(newMockADNL())
		o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0xC1}, 32), 4096, true, false)
		peer := &mockBroadcastPeer{id: bytes.Repeat([]byte{0xC2}, 32)}
		o.EnableBroadcastFECRelay(bytes.Repeat([]byte{0xC3}, 32), mockBroadcastPeerSet{peers: []BroadcastPeer{peer}}, NewBroadcastFECRelayState())
		checked := false
		o.SetBroadcastCheckHandler(func(msg tl.Serializable, info BroadcastInfo) error {
			checked = true
			return nil
		})

		_, priv := keyPairFromSeed(73)
		sender, err := NewBroadcastFECSenderFromTL(priv, CertificateEmpty{}, Message{Overlay: bytes.Repeat([]byte{0xC4}, 32)}, BroadcastFlagAnySender, WithBroadcastFECSymbolSize(256))
		if err != nil {
			t.Fatalf("sender init failed: %v", err)
		}
		part, err := sender.part(0)
		if err != nil {
			t.Fatalf("part build failed: %v", err)
		}

		if err = o.processFECBroadcast(part.full); err != nil {
			t.Fatalf("expected accepted untrusted broadcast, got %v", err)
		}
		if !checked {
			t.Fatalf("expected broadcast check handler to be called")
		}
		if len(peer.sent) != 1 {
			t.Fatalf("accepted untrusted broadcast should be relayed after check, got %d sends", len(peer.sent))
		}
		if _, ok := peer.sent[0].(*BroadcastFEC); !ok {
			t.Fatalf("expected drained full FEC part, got %T", peer.sent[0])
		}
	})
}
