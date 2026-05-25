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
		Date:     1,
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

	finished := signedBroadcastFEC(t, priv, rldp.FECRaptorQ{DataSize: 4, SymbolSize: 2, SymbolsCount: 2}, []byte{1, 2, 3, 4}, 4, BroadcastFlagAnySender)
	id, err := finished.CalcID()
	if err != nil {
		t.Fatalf("calc id failed: %v", err)
	}
	now := time.Now()
	o.broadcastStreams[string(id)] = &fecBroadcastStream{source: priv.Public().(ed25519.PublicKey), finishedAt: &now}

	if err = o.processFECBroadcast(finished); err != nil {
		t.Fatalf("expected finished stream ack path, got err=%v", err)
	}
	if len(m.sendCustomCalls) == 0 {
		t.Fatalf("expected FEC completed ack to be sent")
	}
	if _, ok := m.sendCustomCalls[len(m.sendCustomCalls)-1].(FECCompleted); !ok {
		t.Fatalf("expected FECCompleted message")
	}

	otherPub, otherPriv := keyPairFromSeed(32)
	_ = otherPub
	mismatch := signedBroadcastFEC(t, otherPriv, finished.FEC, finished.Data, finished.DataSize, finished.Flags)
	mismatch.Seqno = finished.Seqno
	mismatch.Date = finished.Date
	if err = o.processFECBroadcast(mismatch); err == nil || !strings.Contains(err.Error(), "malformed source") {
		t.Fatalf("expected malformed source error, got: %v", err)
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
	if err = o.processFECBroadcastShort(part0.short); err != nil {
		t.Fatalf("known short part for partial stream must not fail: %v", err)
	}

	part1, err := sender.part(1)
	if err != nil {
		t.Fatalf("part 1 build failed: %v", err)
	}
	if err = o.processFECBroadcastShort(part1.short); err == nil || !strings.Contains(err.Error(), "unfinished broadcast") {
		t.Fatalf("expected unfinished broadcast error for unseen short part, got: %v", err)
	}
}
