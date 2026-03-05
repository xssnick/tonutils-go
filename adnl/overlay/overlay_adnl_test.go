package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
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

	b := &BroadcastFEC{
		Source:   ed25519Public(priv),
		DataHash: bytes.Repeat([]byte{0xCC}, 32),
		DataSize: dataSize,
		Flags:    flags,
		Data:     data,
		Seqno:    1,
		FEC:      fec,
		Date:     uint32(time.Now().Unix()),
	}

	broadcastHash, err := b.CalcID()
	if err != nil {
		t.Fatalf("calc id failed: %v", err)
	}
	partDataHash := sha256.Sum256(b.Data)
	partHash, err := tl.Hash(&BroadcastFECPartID{BroadcastHash: broadcastHash, DataHash: partDataHash[:], Seqno: b.Seqno})
	if err != nil {
		t.Fatalf("part hash failed: %v", err)
	}
	toSign, err := tl.Serialize(&BroadcastToSign{Hash: partHash, Date: b.Date}, true)
	if err != nil {
		t.Fatalf("serialize sign payload failed: %v", err)
	}
	b.Signature = ed25519.Sign(priv, toSign)
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

	finished := signedBroadcastFEC(t, priv, rldp.FECRaptorQ{DataSize: 4, SymbolSize: 2, SymbolsCount: 2}, []byte{1, 2, 3, 4}, 4, _BroadcastFlagAnySender)
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
